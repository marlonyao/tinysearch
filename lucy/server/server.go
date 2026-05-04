package server

import (
	"encoding/json"
	"net/http"

	"lucy/analysis"
	"lucy/document"
	"lucy/index"
	"lucy/search"
)

// Server HTTP 搜索引擎服务
type Server struct {
	indexWriter *index.IndexWriter
	indexSearcher *search.IndexSearcher
	queryParser   *search.QueryParser
}

// NewServer 创建 HTTP 服务
func NewServer() *Server {
	tok := analysis.StandardTokenizer{}
	writer := index.NewIndexWriter(tok)
	return &Server{
		indexWriter:   writer,
		indexSearcher: search.NewIndexSearcher(writer.Index()),
		queryParser:   search.NewQueryParser(),
	}
}

// IndexDocumentRequest 索引文档请求
type IndexDocumentRequest struct {
	Fields map[string]IndexField `json:"fields"`
}

// IndexField 单个字段定义
type IndexField struct {
	Value     string `json:"value"`
	Store     bool   `json:"store,omitempty"`
	Index     bool   `json:"index,omitempty"`
	Tokenize  bool   `json:"tokenize,omitempty"`
}

// IndexDocumentResponse 索引响应
type IndexDocumentResponse struct {
	DocID uint32 `json:"doc_id"`
}

// handleIndex 处理文档索引请求
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req IndexDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	doc := document.NewDocument()
	for name, f := range req.Fields {
		var attr document.FieldAttribute
		if f.Store {
			attr |= document.Store
		}
		if f.Index {
			attr |= document.Index
		}
		if f.Tokenize {
			attr |= document.Tokenize
		}
		// 默认行为：如果不指定，假设 Index=true, Tokenize=true, Store=false
		if attr == 0 {
			attr = document.Index | document.Tokenize
		}
		doc.Add(document.NewField(name, f.Value, attr))
	}

	s.indexWriter.AddDocument(doc)
	// 更新 searcher 的索引引用
	s.indexSearcher = search.NewIndexSearcher(s.indexWriter.Index())

	resp := IndexDocumentResponse{DocID: doc.ID}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// SearchResult 搜索结果
type SearchResult struct {
	TotalHits int                 `json:"total"`
	Docs      []SearchResultDoc   `json:"docs"`
}

// SearchResultDoc 单个结果文档
type SearchResultDoc struct {
	DocID uint32  `json:"doc_id"`
	Score float64 `json:"score"`
}

// handleSearch 处理搜索请求
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "Missing query parameter 'q'", http.StatusBadRequest)
		return
	}

	query := s.queryParser.Parse(q)
	result := query.Execute(s.indexSearcher)

	var docs []SearchResultDoc
	for _, sd := range result.ScoreDocs {
		docs = append(docs, SearchResultDoc{
			DocID: sd.DocID,
			Score: sd.Score,
		})
	}

	resp := SearchResult{
		TotalHits: result.TotalHits,
		Docs:      docs,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// StatsResponse 索引统计
type StatsResponse struct {
	DocCount uint32 `json:"doc_count"`
}

// handleStats 返回索引统计
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := StatsResponse{DocCount: s.indexWriter.DocCount()}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Handler 返回 HTTP handler
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/index", s.handleIndex)
	mux.HandleFunc("/search", s.handleSearch)
	mux.HandleFunc("/stats", s.handleStats)
	return mux
}

// ListenAndServe 启动 HTTP 服务
func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.Handler())
}
