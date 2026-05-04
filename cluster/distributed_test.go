package cluster

import (
	"fmt"
	"testing"
	"time"

	"lucy/document"
)

// TestDistributedIndex 跨节点转发写入：协调节点不是 primary 所在节点
func TestDistributedIndex(t *testing.T) {
	hub := NewTransportHub()

	// 2 节点
	mt := hub.CreateTransport("master")
	master := NewNode("master", mt)
	master.Start()
	defer master.Stop()
	master.Join("")

	w1t := hub.CreateTransport("w1")
	w1 := NewNode("w1", w1t)
	w1.Start()
	defer w1.Stop()
	w1.Join("master")

	// 等待同步
	for _, n := range []*Node{master, w1} {
		if !n.WaitForStateVersion(2, time.Second) {
			t.Fatalf("node %s did not sync", n.ID)
		}
	}

	// 创建 2 shard, 0 replica（master 和 w1 各一个 primary）
	master.CreateIndex("products", 2, 0)
	for _, n := range []*Node{master, w1} {
		if !n.WaitForStateVersion(3, time.Second) {
			t.Fatalf("node %s did not receive index", n.ID)
		}
	}
	// 等待 shard 启动
	time.Sleep(100 * time.Millisecond)

	// 找一个路由到 w1 的文档
	var targetDocID string
	for i := 0; i < 100; i++ {
		docID := fmt.Sprintf("doc-%d", i)
		_, shard, _ := master.RouteDocument("products", docID)
		if shard != nil && shard.NodeID == "w1" {
			targetDocID = docID
			break
		}
	}
	if targetDocID == "" {
		t.Fatal("could not find doc routing to w1")
	}

	// 从 master（协调节点）写入，应该转发到 w1 并成功
	doc := document.NewDocument()
	doc.Add(document.NewField("title", targetDocID, document.Store|document.Index|document.Tokenize))
	if err := master.IndexDocument("products", targetDocID, doc); err != nil {
		t.Fatalf("distributed index failed: %v", err)
	}

	// 验证 w1 的 shard 中有数据
	_, shardInfo, _ := master.RouteDocument("products", targetDocID)
	engine, ok := w1.shards.GetPrimaryShard("products", shardInfo.ShardID)
	if !ok {
		t.Fatalf("w1 primary shard %d not found", shardInfo.ShardID)
	}
	idx := engine.Writer.Index()
	if idx.DocCount() != 1 {
		t.Fatalf("expected 1 doc in w1 shard, got %d", idx.DocCount())
	}
}

// TestReplicaSync 写入主分片后，副本分片也收到数据
func TestReplicaSync(t *testing.T) {
	hub := NewTransportHub()

	mt := hub.CreateTransport("master")
	master := NewNode("master", mt)
	master.Start()
	defer master.Stop()
	master.Join("")

	w1t := hub.CreateTransport("w1")
	w1 := NewNode("w1", w1t)
	w1.Start()
	defer w1.Stop()
	w1.Join("master")

	// 等待同步
	for _, n := range []*Node{master, w1} {
		if !n.WaitForStateVersion(2, time.Second) {
			t.Fatalf("node %s did not sync", n.ID)
		}
	}

	// 创建 1 shard, 1 replica（master 上 primary，w1 上 replica）
	master.CreateIndex("test", 1, 1)
	for _, n := range []*Node{master, w1} {
		if !n.WaitForStateVersion(3, time.Second) {
			t.Fatalf("node %s did not receive index", n.ID)
		}
	}
	time.Sleep(100 * time.Millisecond)

	// 写入 master（primary 所在）
	doc := document.NewDocument()
	doc.Add(document.NewField("title", "hello", document.Store|document.Index|document.Tokenize))
	if err := master.IndexDocument("test", "doc-1", doc); err != nil {
		t.Fatalf("index failed: %v", err)
	}

	// 等待副本同步（异步消息）
	time.Sleep(100 * time.Millisecond)

	// 验证 master 的 primary 有数据
	primary, ok := master.shards.GetPrimaryShard("test", 0)
	if !ok {
		t.Fatal("primary not found on master")
	}
	if primary.Writer.Index().DocCount() != 1 {
		t.Fatalf("expected 1 doc in primary, got %d", primary.Writer.Index().DocCount())
	}

	// 验证 w1 的 replica 也有数据
	replica, ok := w1.shards.GetShard("test", 0, ShardReplica)
	if !ok {
		t.Fatal("replica not found on w1")
	}
	if replica.Writer.Index().DocCount() != 1 {
		t.Fatalf("expected 1 doc in replica, got %d", replica.Writer.Index().DocCount())
	}
}
