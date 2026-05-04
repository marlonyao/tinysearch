package search

import (
	"math"
	"testing"

	"lucy/analysis"
	"lucy/document"
	"lucy/index"
)

// 辅助函数：快速构建索引
func buildIndex(docs [][]string) *index.InvertedIndex {
	tok := analysis.StandardTokenizer{}
	writer := index.NewIndexWriter(tok)
	for _, texts := range docs {
		doc := document.NewDocument()
		for _, text := range texts {
			doc.Add(document.NewField("content", text, document.Store|document.Index|document.Tokenize))
		}
		writer.AddDocument(doc)
	}
	return writer.Index()
}

func TestTermQuery(t *testing.T) {
	// doc0: "hello world"
	// doc1: "hello lucene"
	// doc2: "world world"
	idx := buildIndex([][]string{
		{"hello world"},
		{"hello lucene"},
		{"world world"},
	})

	searcher := NewIndexSearcher(idx)

	q := &TermQuery{Term: "hello"}
	result := q.Execute(searcher)

	if result.TotalHits != 2 {
		t.Fatalf("TotalHits = %d, want 2", result.TotalHits)
	}

	// doc0 和 doc1 都包含 hello，TF 都是 1，IDF 相同，score 相同
	found := make(map[uint32]bool)
	for _, sd := range result.ScoreDocs {
		found[sd.DocID] = true
	}
	if !found[0] || !found[1] {
		t.Errorf("expected doc0 and doc1, got %v", result.ScoreDocs)
	}
}

func TestTermQueryNotFound(t *testing.T) {
	idx := buildIndex([][]string{{"hello"}})
	searcher := NewIndexSearcher(idx)

	q := &TermQuery{Term: "notexist"}
	result := q.Execute(searcher)
	if result.TotalHits != 0 {
		t.Errorf("TotalHits = %d, want 0", result.TotalHits)
	}
}

func TestTermQueryScore(t *testing.T) {
	// doc0: "go search"      — go 出现 1 次
	// doc1: "go go"          — go 出现 2 次
	// doc2: "lucene search"  — 没有 go，让 df(go)=2 < N=3
	idx := buildIndex([][]string{
		{"go search"},
		{"go go"},
		{"lucene search"},
	})

	searcher := NewIndexSearcher(idx)
	q := &TermQuery{Term: "go"}
	result := q.Execute(searcher)

	// 只有 doc0 和 doc1 包含 go
	if result.TotalHits != 2 {
		t.Fatalf("TotalHits = %d, want 2", result.TotalHits)
	}

	// doc1 TF=2, doc0 TF=1，doc1 应该排第一
	if result.ScoreDocs[0].DocID != 1 {
		t.Errorf("top doc = %d, want 1 (TF=2)", result.ScoreDocs[0].DocID)
	}

	// 验证 score 值
	N := 3.0
	df := 2.0
	idf := math.Log(N / df)
	wantScore := 2.0 * idf // doc1 TF=2
	if math.Abs(result.ScoreDocs[0].Score-wantScore) > 1e-9 {
		t.Errorf("top score = %v, want %v", result.ScoreDocs[0].Score, wantScore)
	}
}

func TestBooleanQueryMust(t *testing.T) {
	// doc0: "hello world"
	// doc1: "hello lucene"
	// doc2: "world lucene"
	idx := buildIndex([][]string{
		{"hello world"},
		{"hello lucene"},
		{"world lucene"},
	})

	searcher := NewIndexSearcher(idx)
	q := &BooleanQuery{
		Clauses: []BooleanClause{
			{Query: &TermQuery{Term: "hello"}, Occur: MUST},
			{Query: &TermQuery{Term: "world"}, Occur: MUST},
		},
	}
	result := q.Execute(searcher)

	// 只有 doc0 同时包含 hello 和 world
	if result.TotalHits != 1 {
		t.Fatalf("TotalHits = %d, want 1", result.TotalHits)
	}
	if result.ScoreDocs[0].DocID != 0 {
		t.Errorf("DocID = %d, want 0", result.ScoreDocs[0].DocID)
	}
}

func TestBooleanQueryShould(t *testing.T) {
	// doc0: "hello"
	// doc1: "world"
	// doc2: "hello world"
	idx := buildIndex([][]string{
		{"hello"},
		{"world"},
		{"hello world"},
	})

	searcher := NewIndexSearcher(idx)
	q := &BooleanQuery{
		Clauses: []BooleanClause{
			{Query: &TermQuery{Term: "hello"}, Occur: SHOULD},
			{Query: &TermQuery{Term: "world"}, Occur: SHOULD},
		},
	}
	result := q.Execute(searcher)

	// 三篇文档至少包含一个
	if result.TotalHits != 3 {
		t.Fatalf("TotalHits = %d, want 3", result.TotalHits)
	}

	// doc2 同时包含两个，score 累加，应该排第一
	if result.ScoreDocs[0].DocID != 2 {
		t.Errorf("top doc = %d, want 2", result.ScoreDocs[0].DocID)
	}
}

func TestBooleanQueryMixed(t *testing.T) {
	// doc0: "hello world go"
	// doc1: "hello go"
	// doc2: "world go"
	// doc3: "nothing here" — 没有 go，让 df(go)=3 < N=4
	idx := buildIndex([][]string{
		{"hello world go"},
		{"hello go"},
		{"world go"},
		{"nothing here"},
	})

	searcher := NewIndexSearcher(idx)
	q := &BooleanQuery{
		Clauses: []BooleanClause{
			{Query: &TermQuery{Term: "go"}, Occur: MUST},       // 必须有 go
			{Query: &TermQuery{Term: "hello"}, Occur: SHOULD}, // 有 hello 加分
		},
	}
	result := q.Execute(searcher)

	// 所有文档都有 go，所以都在结果里
	if result.TotalHits != 3 {
		t.Fatalf("TotalHits = %d, want 3", result.TotalHits)
	}

	// doc0 和 doc1 都有 hello+go，doc2 只有 go
	// doc2 的 score 应该最低（没有 hello 加分）
	if result.ScoreDocs[len(result.ScoreDocs)-1].DocID != 2 {
		t.Errorf("last doc = %d, want 2 (no hello)", result.ScoreDocs[len(result.ScoreDocs)-1].DocID)
	}

	// 验证三篇文档都在结果里
	found := make(map[uint32]bool)
	for _, sd := range result.ScoreDocs {
		found[sd.DocID] = true
	}
	if !found[0] || !found[1] || !found[2] {
		t.Errorf("expected doc0,1,2 all present, got %v", result.ScoreDocs)
	}
}

func TestBooleanQueryMustNot(t *testing.T) {
	// doc0: "hello world"
	// doc1: "hello lucene"
	// doc2: "world lucene"
	idx := buildIndex([][]string{
		{"hello world"},
		{"hello lucene"},
		{"world lucene"},
	})

	searcher := NewIndexSearcher(idx)
	q := &BooleanQuery{
		Clauses: []BooleanClause{
			{Query: &TermQuery{Term: "hello"}, Occur: MUST},       // 必须有 hello
			{Query: &TermQuery{Term: "world"}, Occur: MUST_NOT},   // 不能有 world
		},
	}
	result := q.Execute(searcher)

	// doc0 有 hello 和 world → 被排除
	// doc1 有 hello 没有 world → 命中
	// doc2 没有 hello → 不满足 MUST
	if result.TotalHits != 1 {
		t.Fatalf("TotalHits = %d, want 1", result.TotalHits)
	}
	if result.ScoreDocs[0].DocID != 1 {
		t.Errorf("DocID = %d, want 1", result.ScoreDocs[0].DocID)
	}
}

func TestPhraseQuery(t *testing.T) {
	// doc0: "hello world"        → hello@0, world@1
	// doc1: "world hello"        → world@0, hello@1
	// doc2: "hello lucene world" → hello@0, lucene@1, world@2
	idx := buildIndex([][]string{
		{"hello world"},
		{"world hello"},
		{"hello lucene world"},
	})

	searcher := NewIndexSearcher(idx)
	q := &PhraseQuery{Terms: []string{"hello", "world"}}
	result := q.Execute(searcher)

	// 只有 doc0 和 doc2 同时有 hello 和 world
	// doc0: hello@0, world@1 → 连续 ✓
	// doc2: hello@0, world@2 → 不连续 ✗
	// doc1: hello@1, world@0 → 不连续 ✗
	if result.TotalHits != 1 {
		t.Fatalf("TotalHits = %d, want 1", result.TotalHits)
	}
	if result.ScoreDocs[0].DocID != 0 {
		t.Errorf("DocID = %d, want 0", result.ScoreDocs[0].DocID)
	}
}

func TestPhraseQueryNotFound(t *testing.T) {
	idx := buildIndex([][]string{{"hello world"}})
	searcher := NewIndexSearcher(idx)

	q := &PhraseQuery{Terms: []string{"hello", "lucene"}}
	result := q.Execute(searcher)
	if result.TotalHits != 0 {
		t.Errorf("TotalHits = %d, want 0", result.TotalHits)
	}
}

func TestTopDocsSortOrder(t *testing.T) {
	// doc0: "a b"         — a 出现 1 次
	// doc1: "a a a c"     — a 出现 3 次
	// doc2: "a a d"       — a 出现 2 次
	// doc3: "e f"         — 没有 a，让 df(a)=3 < N=4
	idx := buildIndex([][]string{
		{"a b"},
		{"a a a c"},
		{"a a d"},
		{"e f"},
	})

	searcher := NewIndexSearcher(idx)
	q := &TermQuery{Term: "a"}
	result := q.Execute(searcher)

	// 只有 doc0,1,2 包含 a
	if result.TotalHits != 3 {
		t.Fatalf("TotalHits = %d, want 3", result.TotalHits)
	}

	// TF: doc1=3, doc2=2, doc0=1 → score 递减
	wantOrder := []uint32{1, 2, 0}
	for i, sd := range result.ScoreDocs {
		if sd.DocID != wantOrder[i] {
			t.Errorf("rank[%d] = doc%d, want doc%d", i, sd.DocID, wantOrder[i])
		}
	}
}
