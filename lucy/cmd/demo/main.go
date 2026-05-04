package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"lucy/analysis"
	"lucy/document"
	"lucy/index"
	"lucy/server"
)

func main() {
	fmt.Println("=== Lucy 搜索引擎启动 ===")

	srv := server.NewServer()

	// 启动 HTTP 服务（后台）
	go srv.ListenAndServe(":8080")
	fmt.Println("HTTP 服务启动: http://localhost:8080")
	time.Sleep(100 * time.Millisecond)

	// 索引一些文档
	fmt.Println("\n=== 索引文档 ===")
	docs := []struct {
		title   string
		content string
	}{
		{"Go 入门", "Go 是一门简洁高效的编程语言，适合后端开发"},
		{"Rust 教程", "Rust 以内存安全著称，学习曲线较陡但性能极致"},
		{"搜索引擎原理", "倒排索引是搜索引擎的核心数据结构，将词映射到文档"},
		{"Go 并发模型", "Go 的 goroutine 和 channel 让并发编程变得简单"},
		{"Lucene 介绍", "Lucene 是 Apache 的搜索引擎库，支持倒排索引和布尔查询"},
	}

	for i, d := range docs {
		req := server.IndexDocumentRequest{
			Fields: map[string]server.IndexField{
				"title":   {Value: d.title, Store: true, Index: true, Tokenize: true},
				"content": {Value: d.content, Store: true, Index: true, Tokenize: true},
			},
		}
		body, _ := json.Marshal(req)
		resp, err := http.Post("http://localhost:8080/index", "application/json", bytes.NewReader(body))
		if err != nil {
			fmt.Printf("索引失败[%d]: %v\n", i, err)
			continue
		}
		resp.Body.Close()
		fmt.Printf("✓ 索引文档[%d]: %s\n", i, d.title)
	}

	// 查看统计
	fmt.Println("\n=== 索引统计 ===")
	resp, _ := http.Get("http://localhost:8080/stats")
	var stats server.StatsResponse
	json.NewDecoder(resp.Body).Decode(&stats)
	resp.Body.Close()
	fmt.Printf("文档总数: %d\n", stats.DocCount)

	// 执行搜索
	queries := []string{
		"Go",
		"Rust",
		"搜索引擎",
		"并发",
		"倒排索引",
		"Go AND 并发",
		"Rust OR Go",
		"内存安全",
	}

	fmt.Println("\n=== 搜索测试 ===")
	for _, q := range queries {
		resp, err := http.Get(fmt.Sprintf("http://localhost:8080/search?q=%s", url.QueryEscape(q)))
		if err != nil {
			fmt.Printf("[%s] 请求失败: %v\n", q, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result server.SearchResult
		json.Unmarshal(body, &result)
		fmt.Printf("\n🔍 '%s' → %d 个结果\n", q, result.TotalHits)
		for _, doc := range result.Docs {
			fmt.Printf("   文档[%d] 得分: %.4f\n", doc.DocID, doc.Score)
		}
	}

	// 演示磁盘持久化：直接用底层 index 包
	fmt.Println("\n=== 磁盘持久化 ===")
	indexPath := "/tmp/lucy_demo.index"
	
	// 由于 server 没暴露 writer，我们直接创建一个 writer 来演示 Commit/Load
	tok := analysis.StandardTokenizer{}
	writer := index.NewIndexWriter(tok)
	
	// 索引同样的文档
	for _, d := range docs {
		doc := document.NewDocument()
		doc.Add(document.NewField("title", d.title, document.Store|document.Index|document.Tokenize))
		doc.Add(document.NewField("content", d.content, document.Store|document.Index|document.Tokenize))
		writer.AddDocument(doc)
	}
	
	if err := writer.Commit(indexPath); err != nil {
		fmt.Printf("保存失败: %v\n", err)
	} else {
		fmt.Printf("✓ 索引已保存到: %s\n", indexPath)
	}

	// 验证加载
	writer2 := index.NewIndexWriter(tok)
	if err := writer2.LoadFromFile(indexPath); err != nil {
		fmt.Printf("加载失败: %v\n", err)
	} else {
		idx := writer2.Index()
		fmt.Printf("✓ 从磁盘加载成功，文档数: %d\n", idx.DocCount())
		fmt.Printf("✓ 'Go' 的倒排表文档频率: %d\n", idx.Get("go").DocFreq())
		fmt.Printf("✓ '搜索引擎' 的倒排表文档频率: %d\n", idx.Get("搜索引擎").DocFreq())
	}

	fmt.Println("\n=== 演示完成 ===")
	fmt.Println("清理临时文件...")
	os.Remove(indexPath)
}
