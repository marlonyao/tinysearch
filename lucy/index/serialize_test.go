package index

import (
	"bytes"
	"os"
	"testing"

	"lucy/analysis"
	"lucy/document"
)

func buildTestIndex() *IndexWriter {
	tok := analysis.StandardTokenizer{}
	writer := NewIndexWriter(tok)

	doc0 := document.NewDocument()
	doc0.Add(document.NewField("content", "hello world", document.Store|document.Index|document.Tokenize))
	writer.AddDocument(doc0)

	doc1 := document.NewDocument()
	doc1.Add(document.NewField("content", "hello lucene", document.Store|document.Index|document.Tokenize))
	writer.AddDocument(doc1)

	doc2 := document.NewDocument()
	doc2.Add(document.NewField("content", "world world", document.Store|document.Index|document.Tokenize))
	writer.AddDocument(doc2)

	return writer
}

func TestSerializeRoundTrip(t *testing.T) {
	writer := buildTestIndex()
	original := writer.Index()

	// 序列化到内存 buffer
	var buf bytes.Buffer
	if err := Save(original, &buf); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// 反序列化
	loaded, err := Load(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// 验证 docCount
	if loaded.DocCount() != original.DocCount() {
		t.Errorf("DocCount = %d, want %d", loaded.DocCount(), original.DocCount())
	}

	// 验证 term 数量
	if len(loaded.index) != len(original.index) {
		t.Errorf("term count = %d, want %d", len(loaded.index), len(original.index))
	}

	// 验证每个 term 的 postings
	for term, origPL := range original.index {
		loadedPL := loaded.Get(term)
		if loadedPL == nil {
			t.Errorf("missing term: %s", term)
			continue
		}
		if loadedPL.DocFreq() != origPL.DocFreq() {
			t.Errorf("term %s: DocFreq = %d, want %d", term, loadedPL.DocFreq(), origPL.DocFreq())
		}
		for i := 0; i < origPL.DocFreq(); i++ {
			origP := origPL.Get(i)
			loadedP := loadedPL.Get(i)
			if loadedP.DocID != origP.DocID {
				t.Errorf("term %s posting[%d]: DocID = %d, want %d", term, i, loadedP.DocID, origP.DocID)
			}
			if loadedP.TermFreq != origP.TermFreq {
				t.Errorf("term %s posting[%d]: TermFreq = %d, want %d", term, i, loadedP.TermFreq, origP.TermFreq)
			}
			if len(loadedP.Positions) != len(origP.Positions) {
				t.Errorf("term %s posting[%d]: Positions len = %d, want %d", term, i, len(loadedP.Positions), len(origP.Positions))
			}
			for j, pos := range origP.Positions {
				if loadedP.Positions[j] != pos {
					t.Errorf("term %s posting[%d] pos[%d] = %d, want %d", term, i, j, loadedP.Positions[j], pos)
				}
			}
		}
	}
}

func TestSerializeToFile(t *testing.T) {
	writer := buildTestIndex()
	path := "/tmp/lucy_test_index.bin"
	defer os.Remove(path)

	// Commit 到文件
	if err := writer.Commit(path); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// 验证文件存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("index file not created")
	}

	// 加载到新 writer
	newWriter := NewIndexWriter(analysis.StandardTokenizer{})
	if err := newWriter.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	// 验证数据正确
	loaded := newWriter.Index()
	if loaded.DocCount() != 3 {
		t.Errorf("DocCount = %d, want 3", loaded.DocCount())
	}

	// 验证可以搜索
	pl := loaded.Get("hello")
	if pl == nil || pl.DocFreq() != 2 {
		t.Errorf("'hello' DocFreq = %d, want 2", pl.DocFreq())
	}

	pl = loaded.Get("world")
	if pl == nil || pl.DocFreq() != 2 {
		t.Errorf("'world' DocFreq = %d, want 2", pl.DocFreq())
	}

	// 验证 positions 正确
	pl = loaded.Get("world")
	if pl != nil {
		p0 := pl.Get(0) // doc0: "hello world" → world @ position 1
		if p0.Positions[0] != 1 {
			t.Errorf("doc0 'world' position = %d, want 1", p0.Positions[0])
		}
		p1 := pl.Get(1) // doc2: "world world" → world @ positions 0,1
		if len(p1.Positions) != 2 || p1.Positions[0] != 0 || p1.Positions[1] != 1 {
			t.Errorf("doc2 'world' positions = %v, want [0 1]", p1.Positions)
		}
	}
}

func TestSerializeMagicValidation(t *testing.T) {
	// 写无效 magic
	var buf bytes.Buffer
	buf.Write([]byte("XXXX"))
	buf.Write(make([]byte, 12))

	_, err := Load(&buf)
	if err == nil {
		t.Fatal("expected error for invalid magic")
	}
}

func TestSerializeEmptyIndex(t *testing.T) {
	idx := NewInvertedIndex()
	var buf bytes.Buffer
	if err := Save(idx, &buf); err != nil {
		t.Fatalf("Save empty index failed: %v", err)
	}

	loaded, err := Load(&buf)
	if err != nil {
		t.Fatalf("Load empty index failed: %v", err)
	}
	if loaded.DocCount() != 0 {
		t.Errorf("DocCount = %d, want 0", loaded.DocCount())
	}
	if len(loaded.index) != 0 {
		t.Errorf("term count = %d, want 0", len(loaded.index))
	}
}
