package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"lucy/server"
)

func main() {
	fmt.Println("=== 短语搜索 & NOT 查询 验证 ===")

	srv := server.NewServer()
	go srv.ListenAndServe(":8081")
	time.Sleep(100 * time.Millisecond)

	// 索引文档
	docs := []struct {
		id      string
		content string
	}{
		{"doc0", "hello world"},
		{"doc1", "hello lucene"},
		{"doc2", "world lucene"},
		{"doc3", "hello world lucene"},
		{"doc4", "rust memory safety"},
		{"doc5", "go memory model"},
		{"doc6", "concurrent programming in go"},
	}

	fmt.Println("\n=== 索引文档 ===")
	for i, d := range docs {
		req := server.IndexDocumentRequest{
			Fields: map[string]server.IndexField{
				"id":      {Value: d.id, Store: true, Index: false, Tokenize: false},
				"content": {Value: d.content, Store: true, Index: true, Tokenize: true},
			},
		}
		body, _ := json.Marshal(req)
		resp, _ := http.Post("http://localhost:8081/index", "application/json", bytes.NewReader(body))
		resp.Body.Close()
		fmt.Printf("✓ [%d] %s: %s\n", i, d.id, d.content)
	}

	// 查看统计
	resp, _ := http.Get("http://localhost:8081/stats")
	var stats server.StatsResponse
	json.NewDecoder(resp.Body).Decode(&stats)
	resp.Body.Close()
	fmt.Printf("\n文档总数: %d\n", stats.DocCount)

	// 测试用例
	tests := []struct {
		name     string
		query    string
		expected int
		mustHave []uint32
		mustNot  []uint32
	}{
		{
			name:     "短语搜索: hello world",
			query:    `"hello world"`,
			expected: 2,
			mustHave: []uint32{0, 3},
			mustNot:  []uint32{1, 2},
		},
		{
			name:     "短语搜索: hello lucene",
			query:    `"hello lucene"`,
			expected: 1,
			mustHave: []uint32{1},
			mustNot:  []uint32{0, 2, 3},
		},
		{
			name:     "短语搜索: world lucene",
			query:    `"world lucene"`,
			expected: 2,
			mustHave: []uint32{2, 3},
			mustNot:  []uint32{0, 1},
		},
		{
			name:     "短语搜索: 不存在",
			query:    `"world hello"`,
			expected: 0,
			mustHave: []uint32{},
			mustNot:  []uint32{0, 1, 2, 3},
		},
		{
			name:     "NOT 查询: hello NOT world",
			query:    "hello NOT world",
			expected: 1,
			mustHave: []uint32{1},
			mustNot:  []uint32{0, 2, 3},
		},
		{
			name:     "NOT 查询: lucene NOT hello",
			query:    "lucene NOT hello",
			expected: 1,
			mustHave: []uint32{2},
			mustNot:  []uint32{0, 1, 3},
		},
		{
			name:     "AND + NOT: hello AND lucene NOT world",
			query:    "hello AND lucene NOT world",
			expected: 1,
			mustHave: []uint32{1},
			mustNot:  []uint32{0, 2, 3},
		},
		{
			name:     "简单 term: memory",
			query:    "memory",
			expected: 2,
			mustHave: []uint32{4, 5},
		},
		{
			name:     "默认 AND: hello world (隐式)",
			query:    "hello world",
			expected: 2,
			mustHave: []uint32{0, 3},
			mustNot:  []uint32{1, 2},
		},
		{
			name:     "显式 OR: hello OR rust",
			query:    "hello OR rust",
			expected: 4,
			mustHave: []uint32{0, 1, 3, 4},
			mustNot:  []uint32{2, 5, 6},
		},
	}

	fmt.Println("\n=== 查询测试 ===")
	allPass := true
	for _, tt := range tests {
		u := fmt.Sprintf("http://localhost:8081/search?q=%s", url.QueryEscape(tt.query))
		resp, err := http.Get(u)
		if err != nil {
			fmt.Printf("❌ [%s] 请求失败: %v\n", tt.name, err)
			allPass = false
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result server.SearchResult
		json.Unmarshal(body, &result)

		pass := true
		passStr := "✅"

		if result.TotalHits != tt.expected {
			pass = false
		}

		found := make(map[uint32]bool)
		for _, sd := range result.Docs {
			found[sd.DocID] = true
		}
		for _, id := range tt.mustHave {
			if !found[id] {
				pass = false
			}
		}
		for _, id := range tt.mustNot {
			if found[id] {
				pass = false
			}
		}

		if !pass {
			passStr = "❌"
			allPass = false
		}

		fmt.Printf("\n%s %s\n", passStr, tt.name)
		fmt.Printf("   查询: %s\n", tt.query)
		fmt.Printf("   期望: %d 个结果 (必须有 %v, 必须无 %v)\n", tt.expected, tt.mustHave, tt.mustNot)
		fmt.Printf("   实际: %d 个结果", result.TotalHits)
		if result.TotalHits > 0 {
			fmt.Printf(" → [")
			for i, sd := range result.Docs {
				if i > 0 {
					fmt.Printf(", ")
				}
				fmt.Printf("doc[%d]", sd.DocID)
			}
			fmt.Printf("]")
		}
		fmt.Println()
	}

	fmt.Println("\n=== 验证完成 ===")
	if allPass {
		fmt.Println("✅ 全部通过")
	} else {
		fmt.Println("❌ 有失败项，请检查")
	}
}
