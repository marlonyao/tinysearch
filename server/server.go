package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"es/cluster"
	lucydoc "lucy/document"
)

// ClusterServer HTTP 搜索引擎服务（分布式集群版）
type ClusterServer struct {
	node *cluster.Node
}

// NewClusterServer 创建集群 HTTP 服务
func NewClusterServer(node *cluster.Node) *ClusterServer {
	return &ClusterServer{node: node}
}

// --- 请求/响应结构 ---

// CreateIndexRequest 创建索引请求
type CreateIndexRequest struct {
	NumShards   int `json:"num_shards"`
	NumReplicas int `json:"num_replicas"`
}

// CreateIndexResponse 创建索引响应
type CreateIndexResponse struct {
	Acknowledged bool   `json:"acknowledged"`
	Index        string `json:"index"`
}

// IndexDocumentRequest 索引文档请求（复用 lucy server 的格式）
type IndexDocumentRequest struct {
	Fields map[string]IndexField `json:"fields"`
}

// IndexField 单个字段定义
type IndexField struct {
	Value    string `json:"value"`
	Store    bool   `json:"store,omitempty"`
	Index    bool   `json:"index,omitempty"`
	Tokenize bool   `json:"tokenize,omitempty"`
}

// IndexDocumentResponse 索引响应
type IndexDocumentResponse struct {
	Index  string `json:"_index"`
	ID     string `json:"_id"`
	Result string `json:"result"`
}

// SearchResponse 搜索响应（ES 风格）
type SearchResponse struct {
	Took   int              `json:"took"`
	Hits   SearchHits       `json:"hits"`
	Shards ShardsInfo       `json:"_shards"`
}

type SearchHits struct {
	Total    SearchTotal      `json:"total"`
	MaxScore float64          `json:"max_score"`
	Hits     []SearchHit      `json:"hits"`
}

type SearchTotal struct {
	Value int `json:"value"`
}

type SearchHit struct {
	Index  string                 `json:"_index"`
	ID     string                 `json:"_id"`
	Score  float64                `json:"_score"`
	Source map[string]interface{} `json:"_source"`
}

type ShardsInfo struct {
	Total      int `json:"total"`
	Successful int `json:"successful"`
	Failed     int `json:"failed"`
}

// ClusterHealthResponse 集群健康响应
type ClusterHealthResponse struct {
	ClusterName string `json:"cluster_name"`
	Status      string `json:"status"`
	Nodes       int    `json:"number_of_nodes"`
	ActiveShards int   `json:"active_shards"`
}

// --- HTTP Handlers ---

// handleCreateIndex PUT /:index
func (s *ClusterServer) handleCreateIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	indexName := strings.TrimPrefix(r.URL.Path, "/")
	indexName = strings.TrimSuffix(indexName, "/")

	var req CreateIndexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// 默认配置
		req.NumShards = 5
		req.NumReplicas = 1
	}
	if req.NumShards <= 0 {
		req.NumShards = 5
	}
	if req.NumReplicas < 0 {
		req.NumReplicas = 1
	}

	if err := s.node.CreateIndex(indexName, req.NumShards, req.NumReplicas); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := CreateIndexResponse{
		Acknowledged: true,
		Index:        indexName,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleIndexDocument POST /:index/_doc/:id
func (s *ClusterServer) handleIndexDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 解析路径: /:index/_doc/:id
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[1] != "_doc" {
		http.Error(w, "Invalid URL format, expected /:index/_doc/:id", http.StatusBadRequest)
		return
	}
	indexName := parts[0]
	docID := parts[2]

	var req IndexDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	doc := lucydoc.NewDocument()
	for name, f := range req.Fields {
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
		if attr == 0 {
			attr = lucydoc.Index | lucydoc.Tokenize
		}
		doc.Add(lucydoc.NewField(name, f.Value, attr))
	}

	if err := s.node.IndexDocument(indexName, docID, doc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := IndexDocumentResponse{
		Index:  indexName,
		ID:     docID,
		Result: "created",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleSearch GET /:index/_search?q=query
func (s *ClusterServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 解析路径: /:index/_search
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[1] != "_search" {
		http.Error(w, "Invalid URL format, expected /:index/_search", http.StatusBadRequest)
		return
	}
	indexName := parts[0]

	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "Missing query parameter 'q'", http.StatusBadRequest)
		return
	}

	topK := 10
	if k := r.URL.Query().Get("size"); k != "" {
		if v, err := strconv.Atoi(k); err == nil && v > 0 {
			topK = v
		}
	}

	result, err := s.node.Search(indexName, q, topK)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var hits []SearchHit
	for _, sd := range result.ScoreDocs {
		hits = append(hits, SearchHit{
			Index:  indexName,
			ID:     fmt.Sprintf("doc-%d", sd.DocID),
			Score:  sd.Score,
			Source: map[string]interface{}{"doc_id": sd.DocID},
		})
	}

	maxScore := 0.0
	if len(result.ScoreDocs) > 0 {
		maxScore = result.ScoreDocs[0].Score
	}

	resp := SearchResponse{
		Took: 0, // TODO: 计时
		Hits: SearchHits{
			Total:    SearchTotal{Value: result.TotalHits},
			MaxScore: maxScore,
			Hits:     hits,
		},
		Shards: ShardsInfo{
			Total:      result.TotalHits, // 简化
			Successful: result.TotalHits,
			Failed:     0,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleClusterHealth GET /_cluster/health
func (s *ClusterServer) handleClusterHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state := s.node.ClusterState()
	status := "green"
	if state.NodeCount() < 3 {
		status = "yellow"
	}

	// 统计活跃分片
	activeShards := 0
	for _, shards := range state.Routing.Shards {
		for _, shard := range shards {
			if shard.State == "STARTED" {
				activeShards++
			}
		}
	}

	resp := ClusterHealthResponse{
		ClusterName:  "es-cluster",
		Status:       status,
		Nodes:        state.NodeCount(),
		ActiveShards: activeShards,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleClusterState GET /_cluster/state
func (s *ClusterServer) handleClusterState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state := s.node.ClusterState()
	data, err := state.ToJSON()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// Handler 返回 HTTP handler
func (s *ClusterServer) Handler() http.Handler {
	mux := http.NewServeMux()
	
	// 集群管理
	mux.HandleFunc("/_cluster/health", s.handleClusterHealth)
	mux.HandleFunc("/_cluster/state", s.handleClusterState)
	
	// 索引和文档操作（用通配符 handler 解析路径）
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(r.URL.Path, "/")
		parts := strings.Split(path, "/")
		
		if len(parts) == 0 {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		
		// 集群管理 API
		if parts[0] == "_cluster" {
			if len(parts) == 2 {
				switch parts[1] {
				case "health":
					s.handleClusterHealth(w, r)
					return
				case "state":
					s.handleClusterState(w, r)
					return
				}
			}
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		
		// 索引级 API
		if len(parts) == 1 {
			// PUT /:index
			s.handleCreateIndex(w, r)
			return
		}
		
		if len(parts) >= 3 && parts[1] == "_doc" {
			// POST /:index/_doc/:id
			s.handleIndexDocument(w, r)
			return
		}
		
		if len(parts) >= 2 && parts[1] == "_search" {
			// GET /:index/_search?q=query
			s.handleSearch(w, r)
			return
		}
		
		http.Error(w, "Not found", http.StatusNotFound)
	})
	
	return mux
}

// ListenAndServe 启动 HTTP 服务
func (s *ClusterServer) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.Handler())
}
