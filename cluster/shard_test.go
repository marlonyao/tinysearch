package cluster

import (
	"fmt"
	"testing"
	"time"

	"lucy/document"
)

// TestCreateIndex 创建索引并分配分片
func TestCreateIndex(t *testing.T) {
	hub := NewTransportHub()

	// 3 节点集群
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

	w2t := hub.CreateTransport("w2")
	w2 := NewNode("w2", w2t)
	w2.Start()
	defer w2.Stop()
	w2.Join("master")

	// 等待同步
	for _, n := range []*Node{master, w1, w2} {
		if !n.WaitForStateVersion(3, time.Second) {
			t.Fatalf("node %s did not sync", n.ID)
		}
	}

	// Master 创建索引：3 个 primary shard，1 个 replica
	if err := master.CreateIndex("products", 3, 1); err != nil {
		t.Fatalf("create index failed: %v", err)
	}

	// 等待所有节点收到新状态（版本 +1）
	for _, n := range []*Node{master, w1, w2} {
		if !n.WaitForStateVersion(4, 2*time.Second) {
			t.Fatalf("node %s did not receive index state", n.ID)
		}
	}

	// 验证路由表
	state := master.ClusterState()
	shards := state.Routing.GetShards("products")
	if len(shards) != 6 { // 3 primary + 3 replica
		t.Fatalf("expected 6 shards, got %d", len(shards))
	}

	// 验证每个分片有 primary 和 replica
	for shardID := 0; shardID < 3; shardID++ {
		primary, ok := state.Routing.GetPrimaryShard("products", shardID)
		if !ok {
			t.Fatalf("missing primary for shard %d", shardID)
		}
		if primary.Role != ShardPrimary {
			t.Fatalf("expected primary role")
		}
	}

	// 验证分片分布：每个节点上应该有 2 个分片（3 primary + 3 replica / 3 nodes = 2 per node）
	nodeShardCount := make(map[string]int)
	for _, shard := range shards {
		nodeShardCount[shard.NodeID]++
	}
	for _, n := range []string{"master", "w1", "w2"} {
		if nodeShardCount[n] != 2 {
			t.Fatalf("expected 2 shards on %s, got %d", n, nodeShardCount[n])
		}
	}
}

// TestRouteDocument 文档路由
func TestRouteDocument(t *testing.T) {
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

	// 创建索引
	master.CreateIndex("products", 3, 0) // 3 shards, no replica for simplicity
	for _, n := range []*Node{master, w1} {
		if !n.WaitForStateVersion(3, time.Second) {
			t.Fatalf("node %s did not receive index", n.ID)
		}
	}

	// 测试路由：同一个 docID 始终路由到同一个 shard
	shardID1, shard1, err := master.RouteDocument("products", "doc-abc")
	if err != nil {
		t.Fatalf("route failed: %v", err)
	}
	if shard1 == nil {
		t.Fatal("expected shard info")
	}

	// 同一 docID 再次路由，结果相同
	shardID2, _, err := master.RouteDocument("products", "doc-abc")
	if err != nil {
		t.Fatalf("route failed: %v", err)
	}
	if shardID1 != shardID2 {
		t.Fatalf("same doc should route to same shard: %d vs %d", shardID1, shardID2)
	}

	// 范围在 [0, 2]
	if shardID1 < 0 || shardID1 >= 3 {
		t.Fatalf("shard out of range: %d", shardID1)
	}

	t.Logf("doc-abc -> shard %d on node %s", shardID1, shard1.NodeID)
}

// TestShardAutoStart 节点收到状态后自动启动本地分片
func TestShardAutoStart(t *testing.T) {
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

	// 创建索引
	master.CreateIndex("products", 2, 0)
	for _, n := range []*Node{master, w1} {
		if !n.WaitForStateVersion(3, time.Second) {
			t.Fatalf("node %s did not receive index", n.ID)
		}
	}

	// 等待 shard 启动（同步是异步的）
	time.Sleep(100 * time.Millisecond)

	// 验证每个节点上有 1 个分片（2 primary / 2 nodes = 1 per node）
	for _, n := range []*Node{master, w1} {
		shards := n.Shards()
		if len(shards) != 1 {
			t.Fatalf("node %s expected 1 shard, got %d", n.ID, len(shards))
		}
		if shards[0].Index != "products" {
			t.Fatalf("expected 'products', got %s", shards[0].Index)
		}
		if shards[0].Role != ShardPrimary {
			t.Fatalf("expected primary role")
		}
	}
}

// TestIndexDocument 写入文档到本地分片
func TestIndexDocument(t *testing.T) {
	hub := NewTransportHub()

	mt := hub.CreateTransport("master")
	master := NewNode("master", mt)
	master.Start()
	defer master.Stop()
	master.Join("")

	// 等待 Master 自举
	if !master.WaitForStateVersion(2, time.Second) {
		t.Fatal("master did not bootstrap")
	}

	// 创建单分片索引（确保文档落在本地）
	master.CreateIndex("test", 1, 0)
	if !master.WaitForStateVersion(3, time.Second) {
		t.Fatal("did not create index")
	}

	// 等待 shard 启动
	time.Sleep(50 * time.Millisecond)

	// 写入文档
	doc := document.NewDocument()
	doc.Add(document.NewField("title", "hello world", document.Store|document.Index|document.Tokenize))

	if err := master.IndexDocument("test", "doc-1", doc); err != nil {
		t.Fatalf("index document failed: %v", err)
	}

	// 验证分片中有数据
	engine, ok := master.shards.GetPrimaryShard("test", 0)
	if !ok {
		t.Fatal("shard not found")
	}

	// 通过 lucy 搜索验证
	idx := engine.Writer.Index()
	if idx.DocCount() != 1 {
		t.Fatalf("expected 1 doc in shard, got %d", idx.DocCount())
	}
}

// TestRouteDocumentToDifferentNode 文档路由到另一个节点
func TestRouteDocumentToDifferentNode(t *testing.T) {
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

	// 创建 2 shard 索引
	master.CreateIndex("products", 2, 0)
	for _, n := range []*Node{master, w1} {
		if !n.WaitForStateVersion(3, time.Second) {
			t.Fatalf("node %s did not receive index", n.ID)
		}
	}

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

	// 从 master 写入该文档，应该报错（shard 不在本地）
	doc := document.NewDocument()
	doc.ID = 0 // lucy 会重新分配
	doc.Add(document.NewField("title", targetDocID, document.Store|document.Index|document.Tokenize))

	err := master.IndexDocument("products", targetDocID, doc)
	if err == nil {
		t.Fatal("expected error when routing to remote node")
	}

	t.Logf("expected error: %v", err)
}