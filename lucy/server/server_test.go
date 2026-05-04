package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServerIndexAndSearch(t *testing.T) {
	srv := NewServer()
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	// 1. 索引文档
	doc0 := IndexDocumentRequest{
		Fields: map[string]IndexField{
			"title": {Value: "Hello World", Index: true, Tokenize: true, Store: true},
		},
	}
	body, _ := json.Marshal(doc0)
	resp, err := http.Post(server.URL+"/index", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("index request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index status = %d, want 200", resp.StatusCode)
	}
	var indexResp IndexDocumentResponse
	json.NewDecoder(resp.Body).Decode(&indexResp)
	resp.Body.Close()
	if indexResp.DocID != 0 {
		t.Errorf("doc_id = %d, want 0", indexResp.DocID)
	}

	// 2. 再索引一篇
	doc1 := IndexDocumentRequest{
		Fields: map[string]IndexField{
			"title": {Value: "Go Programming", Index: true, Tokenize: true},
		},
	}
	body, _ = json.Marshal(doc1)
	resp, _ = http.Post(server.URL+"/index", "application/json", bytes.NewReader(body))
	json.NewDecoder(resp.Body).Decode(&indexResp)
	resp.Body.Close()
	if indexResp.DocID != 1 {
		t.Errorf("doc_id = %d, want 1", indexResp.DocID)
	}

	// 3. 搜索
	resp, err = http.Get(server.URL + "/search?q=hello")
	if err != nil {
		t.Fatalf("search request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status = %d, want 200", resp.StatusCode)
	}
	var searchResp SearchResult
	json.NewDecoder(resp.Body).Decode(&searchResp)
	resp.Body.Close()

	if searchResp.TotalHits != 1 {
		t.Fatalf("total = %d, want 1", searchResp.TotalHits)
	}
	if searchResp.Docs[0].DocID != 0 {
		t.Errorf("doc_id = %d, want 0", searchResp.Docs[0].DocID)
	}

	// 4. stats
	resp, err = http.Get(server.URL + "/stats")
	if err != nil {
		t.Fatalf("stats request failed: %v", err)
	}
	var statsResp StatsResponse
	json.NewDecoder(resp.Body).Decode(&statsResp)
	resp.Body.Close()
	if statsResp.DocCount != 2 {
		t.Errorf("doc_count = %d, want 2", statsResp.DocCount)
	}
}

func TestServerSearchBoolean(t *testing.T) {
	srv := NewServer()
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	// 索引 doc0: hello world
	doc0 := IndexDocumentRequest{
		Fields: map[string]IndexField{
			"content": {Value: "hello world", Index: true, Tokenize: true},
		},
	}
	body, _ := json.Marshal(doc0)
	http.Post(server.URL+"/index", "application/json", bytes.NewReader(body))

	// 索引 doc1: hello lucene
	doc1 := IndexDocumentRequest{
		Fields: map[string]IndexField{
			"content": {Value: "hello lucene", Index: true, Tokenize: true},
		},
	}
	body, _ = json.Marshal(doc1)
	http.Post(server.URL+"/index", "application/json", bytes.NewReader(body))

	// 搜索 "hello world"（默认 AND）
	resp, _ := http.Get(server.URL + "/search?q=hello+world")
	var result SearchResult
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if result.TotalHits != 1 || result.Docs[0].DocID != 0 {
		t.Errorf("AND search failed: %v", result)
	}

	// 搜索 "hello OR lucene"
	resp, _ = http.Get(server.URL + "/search?q=hello+OR+lucene")
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if result.TotalHits != 2 {
		t.Errorf("OR search: total = %d, want 2", result.TotalHits)
	}
}

func TestServerSearchPhrase(t *testing.T) {
	srv := NewServer()
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	// 索引
	docs := []string{
		"hello world",
		"world hello",
		"hello lucene world",
	}
	for _, text := range docs {
		doc := IndexDocumentRequest{
			Fields: map[string]IndexField{
				"content": {Value: text, Index: true, Tokenize: true},
			},
		}
		body, _ := json.Marshal(doc)
		http.Post(server.URL+"/index", "application/json", bytes.NewReader(body))
	}

	// 短语搜索
	resp, _ := http.Get(server.URL + "/search?q=%22hello+world%22")
	var result SearchResult
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	// 只有 doc0 有 "hello world" 连续出现
	if result.TotalHits != 1 || result.Docs[0].DocID != 0 {
		t.Errorf("phrase search failed: %v", result)
	}
}

func TestServerSearchNotFound(t *testing.T) {
	srv := NewServer()
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	resp, _ := http.Get(server.URL + "/search?q=notexist")
	var result SearchResult
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if result.TotalHits != 0 {
		t.Errorf("expected 0 hits, got %d", result.TotalHits)
	}
}

func TestServerInvalidMethod(t *testing.T) {
	srv := NewServer()
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	// GET /index 应该 405
	resp, _ := http.Get(server.URL + "/index")
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /index status = %d, want 405", resp.StatusCode)
	}
	resp.Body.Close()

	// POST /search 应该 405
	resp, _ = http.Post(server.URL+"/search", "application/json", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /search status = %d, want 405", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestServerMissingQuery(t *testing.T) {
	srv := NewServer()
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	resp, _ := http.Get(server.URL + "/search")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}
