package cluster

import (
	"encoding/json"
	"sync"
)

// NodeState 节点状态
type NodeState int

const (
	NodeActive  NodeState = iota // 正常运行
	NodeLeaving                  // 正在离开
	NodeOffline                  // 已离线
)

// NodeInfo 集群中记录的节点信息（用于 ClusterState 序列化）
type NodeInfo struct {
	ID      string    `json:"id"`
	Address string    `json:"address"`
	State   NodeState `json:"state"`
}

// IndexMetadata 索引元数据（预留，后续步骤使用）
type IndexMetadata struct {
	Name        string `json:"name"`
	NumShards   int    `json:"num_shards"`
	NumReplicas int    `json:"num_replicas"`
}

// ClusterState 集群全局状态（Master 维护的权威版本）
type ClusterState struct {
	mu sync.RWMutex

	Version uint64                   `json:"version"` // 单调递增，用于判断新旧
	Nodes   map[string]*NodeInfo     `json:"nodes"`   // 节点 ID -> 信息
	Indices map[string]*IndexMetadata `json:"indices"` // 索引名 -> 元数据
	Routing *RoutingTable            `json:"routing"` // 分片路由表

	// MasterNode 当前 Master 节点 ID
	MasterNode string `json:"master_node"`
}

// NewClusterState 创建空集群状态
func NewClusterState() *ClusterState {
	return &ClusterState{
		Version: 0,
		Nodes:   make(map[string]*NodeInfo),
		Indices: make(map[string]*IndexMetadata),
		Routing: NewRoutingTable(),
	}
}

// IncVersion 原子递增版本号（Master 调用）
func (cs *ClusterState) IncVersion() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.Version++
}

// AddNode 添加节点到状态（Master 调用）
func (cs *ClusterState) AddNode(info *NodeInfo) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.Nodes[info.ID] = info
	cs.Version++
}

// RemoveNode 移除节点（Master 调用）
func (cs *ClusterState) RemoveNode(nodeID string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.Nodes, nodeID)
	cs.Version++
}

// DeleteIndex 删除索引（Master 调用）
func (cs *ClusterState) DeleteIndex(indexName string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.Indices, indexName)
	delete(cs.Routing.Shards, indexName)
	cs.Version++
}

// SetMaster 设置 Master 节点
func (cs *ClusterState) SetMaster(nodeID string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.MasterNode = nodeID
	cs.Version++
}

// GetVersion 读取当前版本号
func (cs *ClusterState) GetVersion() uint64 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.Version
}

// Copy 创建深拷贝（用于广播给各节点）
func (cs *ClusterState) Copy() *ClusterState {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	copy := &ClusterState{
		Version:    cs.Version,
		MasterNode:   cs.MasterNode,
		Nodes:        make(map[string]*NodeInfo, len(cs.Nodes)),
		Indices:      make(map[string]*IndexMetadata, len(cs.Indices)),
		Routing:      NewRoutingTable(),
	}
	for id, info := range cs.Nodes {
		copy.Nodes[id] = &NodeInfo{
			ID:      info.ID,
			Address: info.Address,
			State:   info.State,
		}
	}
	for name, meta := range cs.Indices {
		copy.Indices[name] = &IndexMetadata{
			Name:        meta.Name,
			NumShards:   meta.NumShards,
			NumReplicas: meta.NumReplicas,
		}
	}
	for _, shards := range cs.Routing.Shards {
		for _, shard := range shards {
			copy.Routing.AddShard(&ShardInfo{
				Index:   shard.Index,
				ShardID: shard.ShardID,
				Role:    shard.Role,
				NodeID:  shard.NodeID,
				State:   shard.State,
			})
		}
	}
	return copy
}

// ToJSON 序列化为 JSON
func (cs *ClusterState) ToJSON() ([]byte, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return json.Marshal(cs)
}

// FromJSON 从 JSON 反序列化
func (cs *ClusterState) FromJSON(data []byte) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return json.Unmarshal(data, cs)
}

// NodeCount 返回节点数量
func (cs *ClusterState) NodeCount() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.Nodes)
}

// IsNewerThan 比较版本号
func (cs *ClusterState) IsNewerThan(other *ClusterState) bool {
	return cs.GetVersion() > other.GetVersion()
}
