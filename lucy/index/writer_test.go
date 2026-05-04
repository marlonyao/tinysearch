package index

import (
	"reflect"
	"testing"

	"lucy/analysis"
	"lucy/document"
)

func TestIndexWriterAddDocument(t *testing.T) {
	tok := analysis.StandardTokenizer{}
	writer := NewIndexWriter(tok)

	doc := document.NewDocument()
	doc.Add(
		document.NewField("title", "Hello World", document.Store|document.Index|document.Tokenize),
		document.NewField("url", "https://example.com", document.Store),
	)

	writer.AddDocument(doc)

	// doc.ID 应该被分配
	if doc.ID != 0 {
		t.Errorf("doc.ID = %d, want 0", doc.ID)
	}

	ii := writer.Index()

	// title 被分词后有两个 term
	helloPL := ii.Get("hello")
	if helloPL == nil || helloPL.DocFreq() != 1 {
		t.Fatalf("hello DocFreq = %d, want 1", helloPL.DocFreq())
	}
	p := helloPL.Get(0)
	if p.DocID != 0 || p.TermFreq != 1 {
		t.Errorf("hello posting = %+v, want DocID=0 TermFreq=1", p)
	}

	worldPL := ii.Get("world")
	if worldPL == nil || worldPL.DocFreq() != 1 {
		t.Fatalf("world DocFreq = %d, want 1", worldPL.DocFreq())
	}

	// url 不是 Index 字段，不应进入倒排索引
	if ii.Get("https://example.com") != nil {
		t.Error("url should not be indexed")
	}
}

func TestIndexWriterMultipleDocuments(t *testing.T) {
	tok := analysis.StandardTokenizer{}
	writer := NewIndexWriter(tok)

	doc0 := document.NewDocument()
	doc0.Add(document.NewField("title", "Go Search", document.Store|document.Index|document.Tokenize))
	writer.AddDocument(doc0)

	doc1 := document.NewDocument()
	doc1.Add(document.NewField("title", "Go Programming", document.Store|document.Index|document.Tokenize))
	writer.AddDocument(doc1)

	ii := writer.Index()

	// "go" 在两篇文档里都出现
	goPL := ii.Get("go")
	if goPL == nil || goPL.DocFreq() != 2 {
		t.Fatalf("go DocFreq = %d, want 2", goPL.DocFreq())
	}
	ids := []uint32{goPL.Get(0).DocID, goPL.Get(1).DocID}
	if !reflect.DeepEqual(ids, []uint32{0, 1}) {
		t.Errorf("go docIDs = %v, want [0 1]", ids)
	}

	// "search" 只在 doc0
	searchPL := ii.Get("search")
	if searchPL == nil || searchPL.DocFreq() != 1 || searchPL.Get(0).DocID != 0 {
		t.Errorf("search posting wrong")
	}

	// "programming" 只在 doc1
	progPL := ii.Get("programming")
	if progPL == nil || progPL.DocFreq() != 1 || progPL.Get(0).DocID != 1 {
		t.Errorf("programming posting wrong")
	}
}

func TestIndexWriterUntokenizedField(t *testing.T) {
	tok := analysis.StandardTokenizer{}
	writer := NewIndexWriter(tok)

	doc := document.NewDocument()
	doc.Add(
		document.NewField("doc_id", "42", document.Index), // 不分词
	)
	writer.AddDocument(doc)

	ii := writer.Index()

	// 整词 "42" 进入索引
	pl := ii.Get("42")
	if pl == nil || pl.DocFreq() != 1 {
		t.Fatalf("42 DocFreq = %d, want 1", pl.DocFreq())
	}
	if pl.Get(0).TermFreq != 1 || pl.Get(0).Positions[0] != 0 {
		t.Errorf("42 posting = %+v, want TermFreq=1 Positions=[0]", pl.Get(0))
	}

	// "42" 不会被拆成 "4" 和 "2"
	if ii.Get("4") != nil || ii.Get("2") != nil {
		t.Error("untokenized field should not be split")
	}
}

func TestIndexWriterDocCount(t *testing.T) {
	tok := analysis.StandardTokenizer{}
	writer := NewIndexWriter(tok)

	if writer.DocCount() != 0 {
		t.Errorf("initial DocCount = %d, want 0", writer.DocCount())
	}

	writer.AddDocument(document.NewDocument())
	writer.AddDocument(document.NewDocument())

	if writer.DocCount() != 2 {
		t.Errorf("DocCount = %d, want 2", writer.DocCount())
	}
}

func TestIndexWriterPositions(t *testing.T) {
	tok := analysis.StandardTokenizer{}
	writer := NewIndexWriter(tok)

	doc := document.NewDocument()
	doc.Add(document.NewField("content", "hello lucene hello", document.Store|document.Index|document.Tokenize))
	writer.AddDocument(doc)

	ii := writer.Index()

	// "hello" 出现两次，position 0 和 2
	pl := ii.Get("hello")
	if pl == nil || pl.DocFreq() != 1 {
		t.Fatalf("hello DocFreq = %d, want 1", pl.DocFreq())
	}
	p := pl.Get(0)
	if p.TermFreq != 2 {
		t.Errorf("TermFreq = %d, want 2", p.TermFreq)
	}
	wantPos := []uint32{0, 2}
	if !reflect.DeepEqual(p.Positions, wantPos) {
		t.Errorf("Positions = %v, want %v", p.Positions, wantPos)
	}
}
