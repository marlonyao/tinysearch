package cluster

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	lucydoc "lucy/document"
	"lucy/search"
)

// Node 代表 ES 集群中的一个节点
type Node struct {
	ID       string
	Address  string
	IsMaster bool
	State    NodeState

	transport Transport
	shards    *ShardManager

	// 本地缓存的集群状态（可能落后于 Master）
	localState *ClusterState

	// 如果是 Master，维护权威状态
	masterState *ClusterState

	// 等待写入响应的回调通道（key: requestID）
	pendingAcks map[string]chan IndexResponsePayload

	// 等待查询响应的回调通道（key: requestID）
	pendingSearchAcks map[string]chan SearchResponsePayload

	mu     sync.RWMutex
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewNode 创建节点（未启动）
func NewNode(id string, transport Transport) *Node {
	return &Node{
		ID:                id,
		Address:           transport.Address(),
		State:             NodeActive,
		transport:         transport,
		shards:            NewShardManager(),
		localState:        NewClusterState(),
		pendingAcks:       make(map[string]chan IndexResponsePayload),
		pendingSearchAcks: make(map[string]chan SearchResponsePayload),
		stopCh:            make(chan struct{}),
	}
}

// Start 启动节点，处理消息循环
func (n *Node) Start() {
	n.wg.Add(1)
	go n.loop()
}

// Stop 停止节点
func (n *Node) Stop() {
	close(n.stopCh)
	n.wg.Wait()
}

// ClusterState 返回本地缓存的集群状态（只读）
func (n *Node) ClusterState() *ClusterState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.localState.Copy()
}

// Join 请求加入集群（向目标节点发送 Join 请求）
// 如果是第一个节点，直接成为 Master
func (n *Node) Join(seedNodeID string) error {
	if seedNodeID == "" {
		// 第一个节点：自举为 Master
		n.mu.Lock()
		n.IsMaster = true
		n.masterState = NewClusterState()
		n.masterState.SetMaster(n.ID)
		n.masterState.AddNode(&NodeInfo{
			ID:      n.ID,
			Address: n.Address,
			State:   NodeActive,
		})
		n.localState = n.masterState.Copy()
		n.mu.Unlock()
		return nil
	}

	// 向种子节点发送 Join 请求
	payload, _ := json.Marshal(&JoinRequestPayload{
		NodeID:  n.ID,
		Address: n.Address,
	})
	msg := Message{
		Type:    JoinRequest,
		From:    n.ID,
		Payload: payload,
	}
	return n.transport.Send(seedNodeID, msg)
}

// JoinRequestPayload 加入请求负载
type JoinRequestPayload struct {
	NodeID  string `json:"node_id"`
	Address string `json:"address"`
}

// JoinResponsePayload 加入响应负载
type JoinResponsePayload struct {
	Success bool           `json:"success"`
	State   *ClusterState  `json:"state,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// StateUpdatePayload 状态更新负载
type StateUpdatePayload struct {
	State *ClusterState `json:"state"`
}

// DocumentPayload 可序列化的文档表示
type DocumentPayload struct {
	Fields []FieldPayload `json:"fields"`
}

// FieldPayload 可序列化的字段表示
type FieldPayload struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Store    bool   `json:"store"`
	Index    bool   `json:"index"`
	Tokenize bool   `json:"tokenize"`
}

// IndexRequestPayload 写入请求负载
type IndexRequestPayload struct {
	IndexName string          `json:"index_name"`
	DocID     string          `json:"doc_id"`
	Doc       *DocumentPayload `json:"doc"`
}

// IndexResponsePayload 写入响应负载
type IndexResponsePayload struct {
	RequestID string `json:"request_id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

// ReplicaSyncPayload 副本同步负载
type ReplicaSyncPayload struct {
	IndexName string          `json:"index_name"`
	ShardID   int             `json:"shard_id"`
	Doc       *DocumentPayload `json:"doc"`
}

// SearchRequestPayload 查询请求负载
type SearchRequestPayload struct {
	IndexName   string `json:"index_name"`
	ShardID     int    `json:"shard_id"`
	QueryString string `json:"query_string"`
	TopK        int    `json:"top_k"`
}

// SearchResponsePayload 查询响应负载
type SearchResponsePayload struct {
	ShardID     int                   `json:"shard_id"`
	TotalHits   int                   `json:"total_hits"`
	ScoreDocs   []search.ScoreDoc     `json:"score_docs"`
	Success     bool                  `json:"success"`
	Error       string                `json:"error,omitempty"`
}

// SearchResult 汇总后的查询结果（返回给客户端）
type SearchResult struct {
	TotalHits int
	ScoreDocs []search.ScoreDoc
}

// docToPayload 将 lucy Document 转为可序列化格式
func docToPayload(doc *lucydoc.Document) *DocumentPayload {
	var fields []FieldPayload
	for _, f := range doc.Fields {
		fields = append(fields, FieldPayload{
			Name:     f.Name,
			Value:    f.Value,
			Store:    f.IsStored(),
			Index:    f.IsIndexed(),
			Tokenize: f.IsTokenized(),
		})
	}
	return &DocumentPayload{Fields: fields}
}

// payloadToDoc 从序列化格式恢复 lucy Document
func payloadToDoc(payload *DocumentPayload) *lucydoc.Document {
	doc := lucydoc.NewDocument()
	for _, f := range payload.Fields {
		var attr lucydoc.FieldAttribute
		if f.Store {
			attr |= lucydoc.Store
		}
		if f.Index {
			attr |= lucydoc.Index
		}
		if f.Tokenize {
			attr |= lucydoc.Tokenize
		}
		doc.Add(lucydoc.NewField(f.Name, f.Value, attr))
	}
	return doc
}

// loop 消息处理主循环
func (n *Node) loop() {
	defer n.wg.Done()
	inbox := n.transport.Inbox()
	for {
		select {
		case msg := <-inbox:
			n.handleMessage(msg)
		case <-n.stopCh:
			return
		}
	}
}

// handleMessage 处理接收到的消息
func (n *Node) handleMessage(msg Message) {
	switch msg.Type {
	case JoinRequest:
		n.handleJoinRequest(msg)
	case JoinResponse:
		n.handleJoinResponse(msg)
	case StateUpdate:
		n.handleStateUpdate(msg)
	case IndexRequest:
		n.handleIndexRequest(msg)
	case IndexResponse:
		n.handleIndexResponse(msg)
	case ReplicaSync:
		n.handleReplicaSync(msg)
	case SearchRequest:
		n.handleSearchRequest(msg)
	case SearchResponse:
		n.handleSearchResponse(msg)
	}
}

// handleJoinRequest 处理新节点加入请求（只有 Master 处理）
func (n *Node) handleJoinRequest(msg Message) {
	n.mu.RLock()
	isMaster := n.IsMaster
	state := n.masterState
	n.mu.RUnlock()

	if !isMaster || state == nil {
		// 不是 Master，忽略或转发（简化版：忽略）
		return
	}

	var req JoinRequestPayload
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return
	}

	// Master 更新集群状态
	state.AddNode(&NodeInfo{
		ID:      req.NodeID,
		Address: req.Address,
		State:   NodeActive,
	})

	// Master 同时更新本地缓存（自己不会收到广播）
	n.mu.Lock()
	n.localState = n.masterState.Copy()
	n.mu.Unlock()

	// 广播新的集群状态给所有节点
	n.broadcastState()
}

// handleJoinResponse 处理加入响应
func (n *Node) handleJoinResponse(msg Message) {
	var resp JoinResponsePayload
	if err := json.Unmarshal(msg.Payload, &resp); err != nil {
		return
	}
	if !resp.Success {
		fmt.Printf("node %s join failed: %s\n", n.ID, resp.Error)
		return
	}

	n.mu.Lock()
	n.localState = resp.State.Copy()
	n.mu.Unlock()
}

// handleStateUpdate 处理状态更新
func (n *Node) handleStateUpdate(msg Message) {
	var payload StateUpdatePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return
	}

	n.mu.Lock()
	// 只接受更新的版本（防止乱序消息覆盖新状态）
	if payload.State.IsNewerThan(n.localState) {
		n.localState = payload.State.Copy()
		// 同步本地分片
		go n.syncShards()
	}
	n.mu.Unlock()
}

// broadcastState 广播集群状态给所有已知节点（Master 调用）
func (n *Node) broadcastState() {
	n.mu.RLock()
	state := n.masterState.Copy()
	n.mu.RUnlock()

	payload, _ := json.Marshal(&StateUpdatePayload{State: state})
	msg := Message{
		Type:    StateUpdate,
		From:    n.ID,
		Payload: payload,
	}

	for _, node := range state.Nodes {
		if node.ID == n.ID {
			continue
		}
		n.transport.Send(node.ID, msg)
	}
}

// WaitForStateVersion 等待本地状态达到指定版本（测试用）
func (n *Node) WaitForStateVersion(version uint64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if n.ClusterState().GetVersion() >= version {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// CreateIndex Master 创建索引并分配分片
func (n *Node) CreateIndex(name string, numShards, numReplicas int) error {
	n.mu.RLock()
	isMaster := n.IsMaster
	state := n.masterState
	n.mu.RUnlock()

	if !isMaster || state == nil {
		return fmt.Errorf("only master can create index")
	}

	// 1. 注册索引元数据
	meta := &IndexMetadata{
		Name:        name,
		NumShards:   numShards,
		NumReplicas: numReplicas,
	}

	// 2. 分片分配（轮询策略）
	strategy := &RoundRobinAllocation{}
	var nodes []*NodeInfo
	for _, node := range state.Nodes {
		if node.State == NodeActive {
			nodes = append(nodes, node)
		}
	}
	shards := strategy.Allocate(nodes, meta)

	// 3. 更新 Master 状态
	state.mu.Lock()
	state.Indices[name] = meta
	for _, shard := range shards {
		shard.State = "STARTED"
		state.Routing.AddShard(shard)
	}
	state.Version++
	state.mu.Unlock()

	// 4. 同步本地缓存并广播
	n.mu.Lock()
	n.localState = n.masterState.Copy()
	n.mu.Unlock()
	n.syncShards() // Master 启动自己的分片
	n.broadcastState()

	return nil
}

// RouteDocument 计算文档应该写入哪个分片
func (n *Node) RouteDocument(indexName, docID string) (int, *ShardInfo, error) {
	state := n.ClusterState()
	meta, ok := state.Indices[indexName]
	if !ok {
		return 0, nil, fmt.Errorf("index not found: %s", indexName)
	}

	shardID := RouteDocument(indexName, docID, meta.NumShards)
	shard, ok := state.Routing.GetPrimaryShard(indexName, shardID)
	if !ok {
		return 0, nil, fmt.Errorf("no primary shard for %s[%d]", indexName, shardID)
	}

	return shardID, shard, nil
}

// IndexDocument 索引单个文档
// 如果是协调节点（非 primary 所在节点）：计算路由，转发到目标节点并等待确认
// 如果是 primary 所在节点：写入 lucy，同步副本，返回成功
func (n *Node) IndexDocument(indexName string, docID string, doc *lucydoc.Document) error {
	shardID, shardInfo, err := n.RouteDocument(indexName, docID)
	if err != nil {
		return err
	}

	// 如果目标分片在本地（本节点是 primary）
	if shardInfo.NodeID == n.ID {
		return n.writeLocalPrimary(indexName, shardID, doc)
	}

	// 转发到目标节点
	return n.forwardIndexRequest(shardInfo.NodeID, indexName, docID, doc)
}

// writeLocalPrimary 在本地主分片写入文档并同步副本
func (n *Node) writeLocalPrimary(indexName string, shardID int, doc *lucydoc.Document) error {
	engine, ok := n.shards.GetPrimaryShard(indexName, shardID)
	if !ok {
		return fmt.Errorf("local primary shard %s[%d] not started", indexName, shardID)
	}
	engine.Writer.AddDocument(doc)

	// 同步副本
	n.syncReplicas(indexName, shardID, doc)
	return nil
}

// forwardIndexRequest 转发写入请求到目标节点并等待响应
func (n *Node) forwardIndexRequest(targetNodeID, indexName, docID string, doc *lucydoc.Document) error {
	reqID := fmt.Sprintf("%s-%d", n.ID, time.Now().UnixNano())
	ackCh := make(chan IndexResponsePayload, 1)

	n.mu.Lock()
	n.pendingAcks[reqID] = ackCh
	n.mu.Unlock()

	payload, _ := json.Marshal(&IndexRequestPayload{
		IndexName: indexName,
		DocID:     docID,
		Doc:       docToPayload(doc),
	})
	msg := Message{
		Type:      IndexRequest,
		From:      n.ID,
		RequestID: reqID,
		Payload:   payload,
	}

	if err := n.transport.Send(targetNodeID, msg); err != nil {
		n.mu.Lock()
		delete(n.pendingAcks, reqID)
		n.mu.Unlock()
		return err
	}

	// 等待响应（带超时）
	select {
	case resp := <-ackCh:
		n.mu.Lock()
		delete(n.pendingAcks, reqID)
		n.mu.Unlock()
		if !resp.Success {
			return fmt.Errorf("remote index failed: %s", resp.Error)
		}
		return nil
	case <-time.After(2 * time.Second):
		n.mu.Lock()
		delete(n.pendingAcks, reqID)
		n.mu.Unlock()
		return fmt.Errorf("index request timeout")
	}
}

// handleIndexRequest 处理写入请求（目标 primary 分片节点）
func (n *Node) handleIndexRequest(msg Message) {
	var req IndexRequestPayload
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		n.sendIndexResponse(msg.From, msg.RequestID, false, err.Error())
		return
	}

	doc := payloadToDoc(req.Doc)
	if err := n.IndexDocument(req.IndexName, req.DocID, doc); err != nil {
		// 如果已经本地写入，会递归调用 forwardIndexRequest，这里需要避免
		// 简化处理：直接尝试本地写入
		shardID, shardInfo, routeErr := n.RouteDocument(req.IndexName, req.DocID)
		if routeErr != nil {
			n.sendIndexResponse(msg.From, msg.RequestID, false, routeErr.Error())
			return
		}
		if shardInfo.NodeID != n.ID {
			n.sendIndexResponse(msg.From, msg.RequestID, false, "shard not local")
			return
		}
		engine, ok := n.shards.GetPrimaryShard(req.IndexName, shardID)
		if !ok {
			n.sendIndexResponse(msg.From, msg.RequestID, false, "shard not started")
			return
		}
		engine.Writer.AddDocument(doc)
		n.syncReplicas(req.IndexName, shardID, doc)
	}

	n.sendIndexResponse(msg.From, msg.RequestID, true, "")
}

// sendIndexResponse 发送写入响应
func (n *Node) sendIndexResponse(to, requestID string, success bool, errMsg string) {
	payload, _ := json.Marshal(&IndexResponsePayload{
		RequestID: requestID,
		Success:   success,
		Error:     errMsg,
	})
	msg := Message{
		Type:      IndexResponse,
		From:      n.ID,
		RequestID: requestID,
		Payload:   payload,
	}
	n.transport.Send(to, msg)
}

// handleIndexResponse 处理写入响应（协调节点）
func (n *Node) handleIndexResponse(msg Message) {
	var resp IndexResponsePayload
	if err := json.Unmarshal(msg.Payload, &resp); err != nil {
		return
	}

	n.mu.Lock()
	ackCh, ok := n.pendingAcks[resp.RequestID]
	n.mu.Unlock()
	if ok {
		ackCh <- resp
	}
}

// syncReplicas 将文档同步到所有副本分片
func (n *Node) syncReplicas(indexName string, shardID int, doc *lucydoc.Document) {
	state := n.ClusterState()
	shards := state.Routing.GetShards(indexName)

	for _, shard := range shards {
		if shard.ShardID != shardID || shard.Role != ShardReplica {
			continue
		}
		if shard.NodeID == n.ID {
			// 副本在本地
			engine, ok := n.shards.GetShard(indexName, shardID, ShardReplica)
			if ok {
				engine.Writer.AddDocument(doc)
			}
			continue
		}
		// 副本在远程节点
		payload, _ := json.Marshal(&ReplicaSyncPayload{
			IndexName: indexName,
			ShardID:   shardID,
			Doc:       docToPayload(doc),
		})
		msg := Message{
			Type:    ReplicaSync,
			From:    n.ID,
			Payload: payload,
		}
		n.transport.Send(shard.NodeID, msg)
	}
}

// handleReplicaSync 处理副本同步请求
func (n *Node) handleReplicaSync(msg Message) {
	var req ReplicaSyncPayload
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return
	}

	doc := payloadToDoc(req.Doc)
	engine, ok := n.shards.GetShard(req.IndexName, req.ShardID, ShardReplica)
	if !ok {
		fmt.Printf("node %s: replica shard %s[%d] not started\n", n.ID, req.IndexName, req.ShardID)
		return
	}
	engine.Writer.AddDocument(doc)
}

// syncShards 根据集群状态同步本地分片（启动/停止）
func (n *Node) syncShards() {
	state := n.ClusterState()
	myID := n.ID

	// 找出应该在我节点上的分片
	for _, shards := range state.Routing.Shards {
		for _, shard := range shards {
			if shard.NodeID != myID {
				continue
			}
			// 检查本地是否已启动
			_, exists := n.shards.GetShard(shard.Index, shard.ShardID, shard.Role)
			if !exists {
				// 启动本地分片
				_ = n.shards.StartShard(shard)
			}
		}
	}
}

// Shards 返回节点上的分片列表（测试/调试用）
func (n *Node) Shards() []*ShardInfo {
	return n.shards.ListShards()
}

// --- 分布式查询 ---

// Search 分布式查询入口（任意节点都可作为协调节点）
// 流程：
// 1. 解析查询字符串
// 2. 找到索引涉及的所有 shard
// 3. 为每个 shard 选一台节点（primary 或 replica）
// 4. 并行发送 SearchRequest
// 5. 汇总所有 shard 结果，全局排序取 Top-K
func (n *Node) Search(indexName, queryString string, topK int) (*SearchResult, error) {
	state := n.ClusterState()
	meta, ok := state.Indices[indexName]
	if !ok {
		return nil, fmt.Errorf("index not found: %s", indexName)
	}

	// 解析查询（协调节点统一解析，避免各节点重复解析）
	parser := search.NewQueryParser()
	query := parser.Parse(queryString)

	// 为每个 shard 选一个目标节点
	type shardTarget struct {
		shardID int
		nodeID  string
		role    ShardRole
	}
	var targets []shardTarget

	for shardID := 0; shardID < meta.NumShards; shardID++ {
		shards := state.Routing.GetShards(indexName)
		var chosen *ShardInfo
		for _, s := range shards {
			if s.ShardID == shardID {
				// 优先选 replica 分散读取压力，如果没有 replica 选 primary
				if s.Role == ShardReplica {
					chosen = s
					break
				}
				if chosen == nil {
					chosen = s
				}
			}
		}
		if chosen == nil {
			return nil, fmt.Errorf("no available shard for %s[%d]", indexName, shardID)
		}
		targets = append(targets, shardTarget{
			shardID: shardID,
			nodeID:  chosen.NodeID,
			role:    chosen.Role,
		})
	}

	// 并行查询各 shard
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allDocs []search.ScoreDoc
	var queryErr error

	for _, t := range targets {
		wg.Add(1)
		go func(target shardTarget) {
			defer wg.Done()

			var localResult *search.TopDocs

			if target.nodeID == n.ID {
				// 本地执行
				engine, ok := n.shards.GetShard(indexName, target.shardID, target.role)
				if !ok {
					mu.Lock()
					if queryErr == nil {
						queryErr = fmt.Errorf("local shard %s[%d] not found", indexName, target.shardID)
					}
					mu.Unlock()
					return
				}
				localResult = engine.Search(query)
			} else {
				// 远程请求
				result, err := n.sendSearchRequest(target.nodeID, indexName, target.shardID, queryString, topK)
				if err != nil {
					mu.Lock()
					if queryErr == nil {
						queryErr = err
					}
					mu.Unlock()
					return
				}
				if !result.Success {
					mu.Lock()
					if queryErr == nil {
						queryErr = fmt.Errorf("remote search failed: %s", result.Error)
					}
					mu.Unlock()
					return
				}
				localResult = &search.TopDocs{
					TotalHits: result.TotalHits,
					ScoreDocs: result.ScoreDocs,
				}
			}

			mu.Lock()
			allDocs = append(allDocs, localResult.ScoreDocs...)
			mu.Unlock()
		}(t)
	}

	wg.Wait()

	if queryErr != nil {
		return nil, queryErr
	}

	// 全局排序，取 Top-K
	sort.Slice(allDocs, func(i, j int) bool {
		return allDocs[i].Score > allDocs[j].Score
	})
	if len(allDocs) > topK {
		allDocs = allDocs[:topK]
	}

	return &SearchResult{
		TotalHits: len(allDocs),
		ScoreDocs: allDocs,
	}, nil
}

// sendSearchRequest 发送查询请求到远程节点并等待响应
func (n *Node) sendSearchRequest(targetNodeID, indexName string, shardID int, queryString string, topK int) (*SearchResponsePayload, error) {
	reqID := fmt.Sprintf("search-%s-%d", n.ID, time.Now().UnixNano())
	ackCh := make(chan SearchResponsePayload, 1)

	n.mu.Lock()
	// 复用 pendingAcks 存储搜索响应通道（用类型断言区分）
	// 为了简化，单独用 map
	if n.pendingSearchAcks == nil {
		n.pendingSearchAcks = make(map[string]chan SearchResponsePayload)
	}
	n.pendingSearchAcks[reqID] = ackCh
	n.mu.Unlock()

	payload, _ := json.Marshal(&SearchRequestPayload{
		IndexName:   indexName,
		ShardID:     shardID,
		QueryString: queryString,
		TopK:        topK,
	})
	msg := Message{
		Type:      SearchRequest,
		From:      n.ID,
		RequestID: reqID,
		Payload:   payload,
	}

	if err := n.transport.Send(targetNodeID, msg); err != nil {
		n.mu.Lock()
		delete(n.pendingSearchAcks, reqID)
		n.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ackCh:
		n.mu.Lock()
		delete(n.pendingSearchAcks, reqID)
		n.mu.Unlock()
		return &resp, nil
	case <-time.After(3 * time.Second):
		n.mu.Lock()
		delete(n.pendingSearchAcks, reqID)
		n.mu.Unlock()
		return nil, fmt.Errorf("search request timeout")
	}
}

// handleSearchRequest 处理查询请求（目标分片节点）
func (n *Node) handleSearchRequest(msg Message) {
	var req SearchRequestPayload
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		n.sendSearchResponse(msg.From, msg.RequestID, false, nil, 0, err.Error())
		return
	}

	// 找到本地分片
	state := n.ClusterState()
	shards := state.Routing.GetShards(req.IndexName)
	var targetShard *ShardInfo
	for _, s := range shards {
		if s.ShardID == req.ShardID && s.NodeID == n.ID {
			targetShard = s
			break
		}
	}
	if targetShard == nil {
		n.sendSearchResponse(msg.From, msg.RequestID, false, nil, 0, "shard not local")
		return
	}

	engine, ok := n.shards.GetShard(req.IndexName, req.ShardID, targetShard.Role)
	if !ok {
		n.sendSearchResponse(msg.From, msg.RequestID, false, nil, 0, "shard not started")
		return
	}

	// 解析查询并执行
	parser := search.NewQueryParser()
	query := parser.Parse(req.QueryString)
	result := engine.Search(query)

	// 只返回 Top-K（在本地先截断，减少网络传输）
	docs := result.ScoreDocs
	if len(docs) > req.TopK {
		docs = docs[:req.TopK]
	}

	n.sendSearchResponse(msg.From, msg.RequestID, true, docs, result.TotalHits, "")
}

// sendSearchResponse 发送查询响应
func (n *Node) sendSearchResponse(to, requestID string, success bool, docs []search.ScoreDoc, totalHits int, errMsg string) {
	payload, _ := json.Marshal(&SearchResponsePayload{
		ShardID:   0, // 由接收方从请求中关联
		TotalHits: totalHits,
		ScoreDocs: docs,
		Success:   success,
		Error:     errMsg,
	})
	msg := Message{
		Type:      SearchResponse,
		From:      n.ID,
		RequestID: requestID,
		Payload:   payload,
	}
	n.transport.Send(to, msg)
}

// handleSearchResponse 处理查询响应（协调节点）
func (n *Node) handleSearchResponse(msg Message) {
	var resp SearchResponsePayload
	if err := json.Unmarshal(msg.Payload, &resp); err != nil {
		return
	}

	n.mu.Lock()
	ackCh, ok := n.pendingSearchAcks[msg.RequestID]
	n.mu.Unlock()
	if ok {
		ackCh <- resp
	}
}
