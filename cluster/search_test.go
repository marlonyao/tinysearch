package cluster

import (
	"testing"
	"time"

	lucydoc "lucy/document"
)

// TestDistributedSearch 验证跨节点分布式查询
func TestDistributedSearch(t *testing.T) {
	hub := NewTransportHub()

	// 3 节点集群
	node0 := NewNode("node-0", hub.CreateTransport("node-0"))
	node1 := NewNode("node-1", hub.CreateTransport("node-1"))
	node2 := NewNode("node-2", hub.CreateTransport("node-2"))

	node0.Start()
	node1.Start()
	node2.Start()
	defer node0.Stop()
	defer node1.Stop()
	defer node2.Stop()

	// node0 自举为 Master
	node0.Join("")

	// node1, node2 加入
	node1.Join("node-0")
	node2.Join("node-0")

	// 等待状态同步（version 会多次递增，等最终稳定）
	if !node2.WaitForStateVersion(3, 2*time.Second) {
		t.Fatal("node2 failed to sync state")
	}

	// Master 创建索引：5 shards, 1 replica
	if err := node0.CreateIndex("products", 5, 1); err != nil {
		t.Fatalf("create index failed: %v", err)
	}

	// 等待所有节点同步并启动分片
	for _, node := range []*Node{node0, node1, node2} {
		if !node.WaitForStateVersion(node0.masterState.GetVersion(), 2*time.Second) {
			t.Fatalf("%s failed to sync index state", node.ID)
		}
	}

	// 验证分片已启动
	for _, node := range []*Node{node0, node1, node2} {
		shards := node.Shards()
		if len(shards) == 0 {
			t.Fatalf("%s has no local shards", node.ID)
		}
		t.Logf("%s has %d shards", node.ID, len(shards))
	}

	// 从不同节点写入文档（覆盖不同 shard）
	docs := []struct {
		node    *Node
		docID   string
		content string
	}{
		{node0, "doc-0", "apple iphone phone"},
		{node1, "doc-1", "samsung galaxy phone"},
		{node2, "doc-2", "apple macbook laptop"},
		{node0, "doc-3", "google pixel phone"},
		{node1, "doc-4", "apple ipad tablet"},
	}

	for _, d := range docs {
		doc := newDoc(d.docID, d.content)
		if err := d.node.IndexDocument("products", d.docID, doc); err != nil {
			t.Fatalf("index %s failed: %v", d.docID, err)
		}
	}

	// 等待副本同步（异步，短暂等待）
	waitForReplicaSync()

	// === 测试 1：从 node0 查询 "apple"，应该返回 3 个文档 ===
	result, err := node0.Search("products", "apple", 10)
	if err != nil {
		t.Fatalf("search from node0 failed: %v", err)
	}
	if result.TotalHits != 3 {
		t.Errorf("expected 3 hits for 'apple', got %d", result.TotalHits)
	}
	for _, sd := range result.ScoreDocs {
		t.Logf("  hit docID=%d score=%.2f", sd.DocID, sd.Score)
	}

	// === 测试 2：从 node1 查询 "phone"，应该返回 3 个文档 ===
	result, err = node1.Search("products", "phone", 10)
	if err != nil {
		t.Fatalf("search from node1 failed: %v", err)
	}
	if result.TotalHits != 3 {
		t.Errorf("expected 3 hits for 'phone', got %d", result.TotalHits)
	}

	// === 测试 3：短语查询 "iphone phone" — doc-0 匹配 ===
	result, err = node2.Search("products", `"iphone phone"`, 10)
	if err != nil {
		t.Fatalf("phrase search from node2 failed: %v", err)
	}
	// 只有 doc-0 包含连续的 "iphone phone"
	if result.TotalHits != 1 {
		t.Errorf("expected 1 hit for phrase 'iphone phone', got %d", result.TotalHits)
	}

	// === 测试 4：Top-K 截断 ===
	result, err = node0.Search("products", "phone", 2)
	if err != nil {
		t.Fatalf("topk search failed: %v", err)
	}
	if result.TotalHits != 2 {
		t.Errorf("expected 2 hits for topk=2, got %d", result.TotalHits)
	}

	// === 测试 5：验证结果按 Score 降序排列 ===
	for i := 1; i < len(result.ScoreDocs); i++ {
		if result.ScoreDocs[i-1].Score < result.ScoreDocs[i].Score {
			t.Errorf("results not sorted by score")
		}
	}
}

// TestSearchWithReplicaFallback 验证 primary 挂掉后 replica 仍可查询
func TestSearchWithReplicaFallback(t *testing.T) {
	hub := NewTransportHub()

	node0 := NewNode("node-0", hub.CreateTransport("node-0"))
	node1 := NewNode("node-1", hub.CreateTransport("node-1"))

	node0.Start()
	node1.Start()
	defer node0.Stop()
	defer node1.Stop()

	node0.Join("")
	node1.Join("node-0")

	if !node1.WaitForStateVersion(2, 2*time.Second) {
		t.Fatal("node1 failed to sync")
	}

	// 2 shards, 1 replica → 4 个分片实例分布在 2 节点
	if err := node0.CreateIndex("test", 2, 1); err != nil {
		t.Fatalf("create index failed: %v", err)
	}

	for _, node := range []*Node{node0, node1} {
		if !node.WaitForStateVersion(node0.masterState.GetVersion(), 2*time.Second) {
			t.Fatalf("%s failed to sync", node.ID)
		}
	}

	// 写入文档
	doc := newDoc("doc-0", "hello world")
	if err := node0.IndexDocument("test", "doc-0", doc); err != nil {
		t.Fatalf("index failed: %v", err)
	}
	waitForReplicaSync()

	// 从 node1 查询（node1 可能持有 replica）
	result, err := node1.Search("test", "hello", 10)
	if err != nil {
		t.Fatalf("search from node1 failed: %v", err)
	}
	if result.TotalHits != 1 {
		t.Errorf("expected 1 hit, got %d", result.TotalHits)
	}
}

// newDoc 辅助函数：创建测试文档
func newDoc(id, content string) *lucydoc.Document {
	doc := lucydoc.NewDocument()
	doc.Add(lucydoc.NewField("id", id, lucydoc.Store))
	doc.Add(lucydoc.NewField("content", content, lucydoc.Store|lucydoc.Index|lucydoc.Tokenize))
	return doc
}

// waitForReplicaSync 等待副本同步完成（测试用短暂延时）
func waitForReplicaSync() {
	// InMemoryTransport 是异步 goroutine 投递，给 100ms 让消息飞过去
	time.Sleep(100 * time.Millisecond)
}
