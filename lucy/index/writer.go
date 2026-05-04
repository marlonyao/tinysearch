package index

import (
	"lucy/analysis"
	"lucy/document"
	"os"
)

// IndexWriter 负责将文档写入倒排索引
type IndexWriter struct {
	index     *InvertedIndex
	tokenizer analysis.Tokenizer
	nextDocID uint32
}

// NewIndexWriter 创建 IndexWriter
func NewIndexWriter(tokenizer analysis.Tokenizer) *IndexWriter {
	return &IndexWriter{
		index:     NewInvertedIndex(),
		tokenizer: tokenizer,
	}
}

// AddDocument 将单个文档加入索引
func (w *IndexWriter) AddDocument(doc *document.Document) {
	doc.ID = w.nextDocID
	w.nextDocID++

	for _, field := range doc.IndexedFields() {
		if !field.IsTokenized() {
			// 不分词：整个字段值作为一个 term
			w.index.Add(doc.ID, field.Value, 0)
			continue
		}
		// 分词：逐 token 加入索引
		tokens := w.tokenizer.Tokenize(field.Value)
		for _, tok := range tokens {
			w.index.Add(doc.ID, tok.Term, uint32(tok.Position))
		}
	}
}

// Commit 将当前索引保存到磁盘文件
func (w *IndexWriter) Commit(path string) error {
	w.index.SetDocCount(w.nextDocID)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return Save(w.index, f)
}

// LoadFromFile 从磁盘文件加载索引
func (w *IndexWriter) LoadFromFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	idx, err := Load(f)
	if err != nil {
		return err
	}
	w.index = idx
	w.nextDocID = idx.DocCount()
	return nil
}
func (w *IndexWriter) Index() *InvertedIndex {
	w.index.SetDocCount(w.nextDocID)
	return w.index
}

// DocCount 返回已索引的文档数
func (w *IndexWriter) DocCount() uint32 {
	return w.nextDocID
}
