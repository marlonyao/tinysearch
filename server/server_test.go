package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"es/cluster"
)

func TestClusterServerCreateIndex(t *testing.T) {
	hub := cluster.NewTransportHub()
	node := cluster.NewNode("test-node", hub.CreateTransport("test-node"))
	node.Start()
	defer node.Stop()
	node.Join("")

	// 等待 Master 状态稳定
	time.Sleep(50 * time.Millisecond)

	srv := NewClusterServer(node)
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	// PUT /products
	reqBody, _ := json.Marshal(CreateIndexRequest{NumShards: 3, NumReplicas: 1})
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/products", bytes.NewReader(reqBody))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create index failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var createResp CreateIndexResponse
	json.NewDecoder(resp.Body).Decode(&createResp)
	resp.Body.Close()

	if !createResp.Acknowledged {
		t.Error("expected acknowledged=true")
	}
	if createResp.Index != "products" {
		t.Errorf("index = %s, want products", createResp.Index)
	}

	// 验证分片已启动
	shards := node.Shards()
	if len(shards) == 0 {
		t.Fatal("node has no shards")
	}
	t.Logf("node has %d shards", len(shards))
}

func TestClusterServerIndexAndSearch(t *testing.T) {
	hub := cluster.NewTransportHub()
	node := cluster.NewNode("test-node", hub.CreateTransport("test-node"))
	node.Start()
	defer node.Stop()
	node.Join("")

	time.Sleep(50 * time.Millisecond)

	// 创建索引
	node.CreateIndex("test", 3, 0) // 3 shards, no replica
	time.Sleep(50 * time.Millisecond)

	srv := NewClusterServer(node)
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	// POST /test/_doc/doc-0
	doc := IndexDocumentRequest{
		Fields: map[string]IndexField{
			"title": {Value: "Hello World", Index: true, Tokenize: true, Store: true},
		},
	}
	body, _ := json.Marshal(doc)
	resp, err := http.Post(server.URL+"/test/_doc/doc-0", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("index failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index status = %d, want 200", resp.StatusCode)
	}
	var indexResp IndexDocumentResponse
	json.NewDecoder(resp.Body).Decode(&indexResp)
	resp.Body.Close()
	if indexResp.Result != "created" {
		t.Errorf("result = %s, want created", indexResp.Result)
	}

	// POST /test/_doc/doc-1
	doc2 := IndexDocumentRequest{
		Fields: map[string]IndexField{
			"title": {Value: "Go Programming", Index: true, Tokenize: true},
		},
	}
	body, _ = json.Marshal(doc2)
	resp, _ = http.Post(server.URL+"/test/_doc/doc-1", "application/json", bytes.NewReader(body))
	resp.Body.Close()

	// GET /test/_search?q=hello
	resp, err = http.Get(server.URL + "/test/_search?q=hello")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status = %d, want 200", resp.StatusCode)
	}
	var searchResp SearchResponse
	json.NewDecoder(resp.Body).Decode(&searchResp)
	resp.Body.Close()

	if searchResp.Hits.Total.Value != 1 {
		t.Fatalf("total = %d, want 1", searchResp.Hits.Total.Value)
	}
	if searchResp.Hits.Hits[0].ID != "doc-0" {
		t.Errorf("doc id = %s, want doc-0", searchResp.Hits.Hits[0].ID)
	}
}

func TestClusterServerSearchBoolean(t *testing.T) {
	hub := cluster.NewTransportHub()
	node := cluster.NewNode("test-node", hub.CreateTransport("test-node"))
	node.Start()
	defer node.Stop()
	node.Join("")

	time.Sleep(50 * time.Millisecond)
	node.CreateIndex("docs", 2, 0)
	time.Sleep(50 * time.Millisecond)

	srv := NewClusterServer(node)
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	// 索引 doc0: hello world
	doc0 := IndexDocumentRequest{
		Fields: map[string]IndexField{
			"content": {Value: "hello world", Index: true, Tokenize: true},
		},
	}
	body, _ := json.Marshal(doc0)
	http.Post(server.URL+"/docs/_doc/doc-0", "application/json", bytes.NewReader(body))

	// 索引 doc1: hello lucene
	doc1 := IndexDocumentRequest{
		Fields: map[string]IndexField{
			"content": {Value: "hello lucene", Index: true, Tokenize: true},
		},
	}
	body, _ = json.Marshal(doc1)
	http.Post(server.URL+"/docs/_doc/doc-1", "application/json", bytes.NewReader(body))

	// 搜索 "hello world"（默认 AND）
	resp, _ := http.Get(server.URL + "/docs/_search?q=hello+world")
	var result SearchResponse
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if result.Hits.Total.Value != 1 || result.Hits.Hits[0].ID != "doc-0" {
		t.Errorf("AND search failed: %v", result)
	}

	// 搜索 "hello OR lucene"
	resp, _ = http.Get(server.URL + "/docs/_search?q=hello+OR+lucene")
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if result.Hits.Total.Value != 2 {
		t.Errorf("OR search: total = %d, want 2", result.Hits.Total.Value)
	}
}

func TestClusterServerSearchPhrase(t *testing.T) {
	hub := cluster.NewTransportHub()
	node := cluster.NewNode("test-node", hub.CreateTransport("test-node"))
	node.Start()
	defer node.Stop()
	node.Join("")

	time.Sleep(50 * time.Millisecond)
	node.CreateIndex("docs", 2, 0)
	time.Sleep(50 * time.Millisecond)

	srv := NewClusterServer(node)
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	// 索引
	texts := []string{
		"hello world",
		"world hello",
		"hello lucene world",
	}
	for i, text := range texts {
		doc := IndexDocumentRequest{
			Fields: map[string]IndexField{
				"content": {Value: text, Index: true, Tokenize: true},
			},
		}
		body, _ := json.Marshal(doc)
		http.Post(server.URL+fmt.Sprintf("/docs/_doc/doc-%d", i), "application/json", bytes.NewReader(body))
	}

	// 短语搜索
	resp, _ := http.Get(server.URL + "/docs/_search?q=%22hello+world%22")
	var result SearchResponse
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if result.Hits.Total.Value != 1 || result.Hits.Hits[0].ID != "doc-0" {
		t.Errorf("phrase search failed: %v", result)
	}
}

func TestClusterServerHealth(t *testing.T) {
	hub := cluster.NewTransportHub()
	node := cluster.NewNode("test-node", hub.CreateTransport("test-node"))
	node.Start()
	defer node.Stop()
	node.Join("")

	time.Sleep(50 * time.Millisecond)

	srv := NewClusterServer(node)
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	// GET /_cluster/health
	resp, err := http.Get(server.URL + "/_cluster/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var health ClusterHealthResponse
	json.NewDecoder(resp.Body).Decode(&health)
	resp.Body.Close()

	if health.Nodes != 1 {
		t.Errorf("nodes = %d, want 1", health.Nodes)
	}
	if health.Status != "yellow" {
		t.Errorf("status = %s, want yellow", health.Status)
	}
}

func TestClusterServerClusterState(t *testing.T) {
	hub := cluster.NewTransportHub()
	node := cluster.NewNode("test-node", hub.CreateTransport("test-node"))
	node.Start()
	defer node.Stop()
	node.Join("")

	time.Sleep(50 * time.Millisecond)
	node.CreateIndex("products", 3, 1)
	time.Sleep(50 * time.Millisecond)

	srv := NewClusterServer(node)
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	// GET /_cluster/state
	resp, err := http.Get(server.URL + "/_cluster/state")
	if err != nil {
		t.Fatalf("state request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var state map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&state)
	resp.Body.Close()

	if state["master_node"] != "test-node" {
		t.Errorf("master_node = %v, want test-node", state["master_node"])
	}
}

func TestClusterServerInvalidMethod(t *testing.T) {
	hub := cluster.NewTransportHub()
	node := cluster.NewNode("test-node", hub.CreateTransport("test-node"))
	node.Start()
	defer node.Stop()
	node.Join("")

	time.Sleep(50 * time.Millisecond)
	node.CreateIndex("test", 2, 0)
	time.Sleep(50 * time.Millisecond)

	srv := NewClusterServer(node)
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	// GET /test/_doc/doc-0 应该 405（只允许 POST）
	resp, _ := http.Get(server.URL + "/test/_doc/doc-0")
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /_doc status = %d, want 405", resp.StatusCode)
	}
	resp.Body.Close()

	// POST /test/_search 应该 405（只允许 GET）
	resp, _ = http.Post(server.URL+"/test/_search", "application/json", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /_search status = %d, want 405", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestClusterServerMissingQuery(t *testing.T) {
	hub := cluster.NewTransportHub()
	node := cluster.NewNode("test-node", hub.CreateTransport("test-node"))
	node.Start()
	defer node.Stop()
	node.Join("")

	time.Sleep(50 * time.Millisecond)
	node.CreateIndex("test", 2, 0)
	time.Sleep(50 * time.Millisecond)

	srv := NewClusterServer(node)
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	// GET /test/_search（缺少 q 参数）
	resp, _ := http.Get(server.URL + "/test/_search")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}
