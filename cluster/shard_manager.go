package cluster

import (
	"fmt"
	"sync"

	"lucy/analysis"
	"lucy/index"
	"lucy/search"
)

// ShardEngine 本地分片引擎（包装 lucy 索引）
type ShardEngine struct {
	Info   *ShardInfo
	Writer *index.IndexWriter
}

// Search 在本地分片上执行查询
func (se *ShardEngine) Search(query search.Query) *search.TopDocs {
	searcher := search.NewIndexSearcher(se.Writer.Index())
	return query.Execute(searcher)
}

// ShardManager 管理节点上的所有本地分片
type ShardManager struct {
	mu     sync.RWMutex
	shards map[string]*ShardEngine // key: "indexName_shardId_role"
}

// NewShardManager 创建分片管理器
func NewShardManager() *ShardManager {
	return &ShardManager{
		shards: make(map[string]*ShardEngine),
	}
}

// StartShard 启动本地分片（创建 lucy IndexWriter）
func (sm *ShardManager) StartShard(info *ShardInfo) error {
	key := shardKey(info)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.shards[key]; exists {
		return fmt.Errorf("shard %s already started", key)
	}

	tok := analysis.StandardTokenizer{}
	writer := index.NewIndexWriter(tok)

	sm.shards[key] = &ShardEngine{
		Info:   info,
		Writer: writer,
	}
	return nil
}

// GetShard 获取本地分片引擎
func (sm *ShardManager) GetShard(indexName string, shardID int, role ShardRole) (*ShardEngine, bool) {
	key := fmt.Sprintf("%s_%d_%d", indexName, shardID, role)

	sm.mu.RLock()
	defer sm.mu.RUnlock()
	engine, ok := sm.shards[key]
	return engine, ok
}

// GetPrimaryShard 获取指定索引分片的主分片
func (sm *ShardManager) GetPrimaryShard(indexName string, shardID int) (*ShardEngine, bool) {
	return sm.GetShard(indexName, shardID, ShardPrimary)
}

// ListShards 列出节点上的所有分片
func (sm *ShardManager) ListShards() []*ShardInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var list []*ShardInfo
	for _, engine := range sm.shards {
		list = append(list, engine.Info)
	}
	return list
}

// RemoveShard 移除本地分片
func (sm *ShardManager) RemoveShard(indexName string, shardID int, role ShardRole) {
	key := fmt.Sprintf("%s_%d_%d", indexName, shardID, role)

	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.shards, key)
}

// shardKey 生成分片唯一键
func shardKey(info *ShardInfo) string {
	return fmt.Sprintf("%s_%d_%d", info.Index, info.ShardID, info.Role)
}
