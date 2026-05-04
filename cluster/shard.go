package cluster

import (
	"hash/fnv"
	"sync"
)

// ShardRole 分片角色
type ShardRole int

const (
	ShardPrimary  ShardRole = iota // 主分片（可写可读）
	ShardReplica                   // 副本分片（只读，同步主分片数据）
)

// ShardInfo 单个分片实例的分配信息
type ShardInfo struct {
	Index      string    `json:"index"`       // 所属索引名
	ShardID    int       `json:"shard_id"`    // 分片编号
	Role       ShardRole `json:"role"`        // primary / replica
	NodeID     string    `json:"node_id"`     // 分配到的节点
	State      string    `json:"state"`       // UNASSIGNED / INITIALIZING / STARTED
}

// RoutingTable 索引到分片的路由表
type RoutingTable struct {
	mu sync.RWMutex

	// IndexName -> []ShardInfo（该索引的所有分片实例）
	Shards map[string][]*ShardInfo `json:"shards"`
}

// NewRoutingTable 创建空路由表
func NewRoutingTable() *RoutingTable {
	return &RoutingTable{
		Shards: make(map[string][]*ShardInfo),
	}
}

// AddShard 添加分片分配信息
func (rt *RoutingTable) AddShard(info *ShardInfo) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.Shards[info.Index] = append(rt.Shards[info.Index], info)
}

// GetShards 获取索引的所有分片
func (rt *RoutingTable) GetShards(indexName string) []*ShardInfo {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.Shards[indexName]
}

// GetPrimaryShard 获取指定索引和分片编号的主分片所在节点
func (rt *RoutingTable) GetPrimaryShard(indexName string, shardID int) (*ShardInfo, bool) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	for _, shard := range rt.Shards[indexName] {
		if shard.ShardID == shardID && shard.Role == ShardPrimary {
			return shard, true
		}
	}
	return nil, false
}

// RouteDocument 计算文档应路由到的分片编号
// ES 实际使用 murmur3，这里用 FNV-1a 简化
func RouteDocument(indexName string, docID string, numShards int) int {
	if numShards <= 0 {
		return 0
	}
	h := fnv.New32a()
	h.Write([]byte(indexName))
	h.Write([]byte(docID))
	return int(h.Sum32()) % numShards
}

// AllocationStrategy 分片分配策略接口
type AllocationStrategy interface {
	// Allocate 为索引分配分片到节点
	// nodes: 当前集群节点列表
	// indexMeta: 索引元数据（名字、分片数、副本数）
	// 返回：分片分配列表
	Allocate(nodes []*NodeInfo, indexMeta *IndexMetadata) []*ShardInfo
}

// RoundRobinAllocation 轮询分配策略
type RoundRobinAllocation struct{}

// Allocate 轮询分配 primary 和 replica
// 规则：
// 1. primary 按节点顺序轮询分配
// 2. replica 放在 primary 的下一个节点（避免同节点）
// 3. 如果节点数 < 副本数+1，允许 replica 和 primary 同节点（降级）
func (r *RoundRobinAllocation) Allocate(nodes []*NodeInfo, indexMeta *IndexMetadata) []*ShardInfo {
	numNodes := len(nodes)
	if numNodes == 0 {
		return nil
	}

	var shards []*ShardInfo
	nodeIdx := 0

	for shardID := 0; shardID < indexMeta.NumShards; shardID++ {
		// Primary
		primaryNode := nodes[nodeIdx%numNodes]
		shards = append(shards, &ShardInfo{
			Index:   indexMeta.Name,
			ShardID: shardID,
			Role:    ShardPrimary,
			NodeID:  primaryNode.ID,
			State:   "UNASSIGNED",
		})
		nodeIdx++

		// Replicas
		for rep := 0; rep < indexMeta.NumReplicas; rep++ {
			// replica 从 primary 的下一个节点开始，尽量分散
			replicaNodeIdx := (nodeIdx + rep) % numNodes
			replicaNode := nodes[replicaNodeIdx]
			shards = append(shards, &ShardInfo{
				Index:   indexMeta.Name,
				ShardID: shardID,
				Role:    ShardReplica,
				NodeID:  replicaNode.ID,
				State:   "UNASSIGNED",
			})
		}
	}

	return shards
}
