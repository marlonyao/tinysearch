package cluster

import (
	"testing"
	"time"
)

// TestSingleNodeBootstrap 单节点启动即成为 Master
func TestSingleNodeBootstrap(t *testing.T) {
	hub := NewTransportHub()
	transport := hub.CreateTransport("node-0")

	node := NewNode("node-0", transport)
	node.Start()
	defer node.Stop()

	// 自举为 Master
	if err := node.Join(""); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	if !node.IsMaster {
		t.Fatal("single node should be master")
	}

	state := node.ClusterState()
	if state.MasterNode != "node-0" {
		t.Fatalf("expected master node-0, got %s", state.MasterNode)
	}
	if state.NodeCount() != 1 {
		t.Fatalf("expected 1 node, got %d", state.NodeCount())
	}
	if state.GetVersion() != 2 { // SetMaster + AddNode = 2 次版本递增
		t.Fatalf("expected version 2, got %d", state.GetVersion())
	}
}

// TestTwoNodeJoin 第二个节点加入集群
func TestTwoNodeJoin(t *testing.T) {
	hub := NewTransportHub()

	// Master 节点
	masterTransport := hub.CreateTransport("master")
	master := NewNode("master", masterTransport)
	master.Start()
	defer master.Stop()
	if err := master.Join(""); err != nil {
		t.Fatalf("master bootstrap failed: %v", err)
	}

	// Worker 节点加入
	workerTransport := hub.CreateTransport("worker-1")
	worker := NewNode("worker-1", workerTransport)
	worker.Start()
	defer worker.Stop()

	if err := worker.Join("master"); err != nil {
		t.Fatalf("worker join failed: %v", err)
	}

	// 等待状态同步
	if !master.WaitForStateVersion(3, time.Second) {
		t.Fatal("master did not update state")
	}
	if !worker.WaitForStateVersion(3, time.Second) {
		t.Fatal("worker did not receive state")
	}

	// 验证 Master 状态包含两个节点
	masterState := master.ClusterState()
	if masterState.NodeCount() != 2 {
		t.Fatalf("expected 2 nodes in master state, got %d", masterState.NodeCount())
	}
	if _, ok := masterState.Nodes["worker-1"]; !ok {
		t.Fatal("worker-1 not in master state")
	}

	// 验证 Worker 收到了完整状态
	workerState := worker.ClusterState()
	if workerState.NodeCount() != 2 {
		t.Fatalf("expected 2 nodes in worker state, got %d", workerState.NodeCount())
	}
	if workerState.MasterNode != "master" {
		t.Fatalf("expected master 'master', got %s", workerState.MasterNode)
	}
}

// TestThreeNodeCluster 三个节点，验证状态传播到所有节点
func TestThreeNodeCluster(t *testing.T) {
	hub := NewTransportHub()

	// Master
	masterTransport := hub.CreateTransport("master")
	master := NewNode("master", masterTransport)
	master.Start()
	defer master.Stop()
	master.Join("")

	// Worker 1
	w1Transport := hub.CreateTransport("w1")
	w1 := NewNode("w1", w1Transport)
	w1.Start()
	defer w1.Stop()
	w1.Join("master")

	// Worker 2
	w2Transport := hub.CreateTransport("w2")
	w2 := NewNode("w2", w2Transport)
	w2.Start()
	defer w2.Stop()
	w2.Join("master")

	// 等待所有节点同步到版本 5（Master 初始 2 + w1 加入 1 + w2 加入 1 + 各次广播...实际版本会到 4 或 5）
	for _, n := range []*Node{master, w1, w2} {
		if !n.WaitForStateVersion(3, time.Second) {
			t.Fatalf("node %s did not sync state", n.ID)
		}
	}

	// 验证所有节点看到相同的集群状态
	for _, n := range []*Node{master, w1, w2} {
		state := n.ClusterState()
		if state.NodeCount() != 3 {
			t.Fatalf("node %s expected 3 nodes, got %d", n.ID, state.NodeCount())
		}
		if state.MasterNode != "master" {
			t.Fatalf("node %s wrong master", n.ID)
		}
	}
}

// TestStateVersionOrdering 版本号比较
func TestStateVersionOrdering(t *testing.T) {
	s1 := NewClusterState()
	s1.Version = 5

	s2 := NewClusterState()
	s2.Version = 10

	if !s2.IsNewerThan(s1) {
		t.Fatal("version 10 should be newer than 5")
	}
	if s1.IsNewerThan(s2) {
		t.Fatal("version 5 should not be newer than 10")
	}
}

// TestNodeJoinRejectedIfNotMaster 非 Master 节点不处理加入请求
func TestNodeJoinRejectedIfNotMaster(t *testing.T) {
	hub := NewTransportHub()

	// Master
	mt := hub.CreateTransport("master")
	master := NewNode("master", mt)
	master.Start()
	defer master.Stop()
	master.Join("")

	// Worker（不是 Master）
	wt := hub.CreateTransport("worker")
	worker := NewNode("worker", wt)
	worker.Start()
	defer worker.Stop()
	// worker 假装也自举，但不会处理 join
	worker.Join("master") // 正确路径

	// 等待
	if !worker.WaitForStateVersion(2, time.Second) {
		t.Fatal("worker should receive state from master")
	}

	// 尝试向 worker 发 join 请求（应该被忽略）
	imposterTransport := hub.CreateTransport("imposter")
	imposter := NewNode("imposter", imposterTransport)
	imposter.Start()
	defer imposter.Stop()
	imposter.Join("worker") // 向非 Master 发请求

	// 等待一段时间，确认状态没变
	time.Sleep(100 * time.Millisecond)
	state := master.ClusterState()
	if state.NodeCount() != 2 { // master + worker，没有 imposter
		t.Fatalf("expected 2 nodes, got %d", state.NodeCount())
	}
}
