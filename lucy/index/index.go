package index

// Posting 倒排表中的一个条目：某个词在某篇文档中的完整信息
type Posting struct {
	DocID     uint32   // 文档ID
	TermFreq  uint32   // 该词在文档中出现次数
	Positions []uint32 // 每次出现的位置（用于短语查询）
}

// PostingList 一个 term 对应的所有文档，按 DocID 升序排列
type PostingList struct {
	postings []Posting
}

// NewPostingList 创建空的 PostingList
func NewPostingList() *PostingList {
	return &PostingList{}
}

// Add 向 PostingList 添加一个 Posting（用于加载）
func (pl *PostingList) Add(docID, termFreq uint32, positions []uint32) {
	pl.postings = append(pl.postings, Posting{
		DocID:     docID,
		TermFreq:  termFreq,
		Positions: positions,
	})
}
func (pl *PostingList) DocFreq() int {
	if pl == nil {
		return 0
	}
	return len(pl.postings)
}

// Get 按索引取 posting
func (pl *PostingList) Get(i int) Posting {
	return pl.postings[i]
}

// Postings 返回底层数组（只读访问）
func (pl *PostingList) Postings() []Posting {
	if pl == nil {
		return nil
	}
	return pl.postings
}

// InvertedIndex 内存倒排索引
type InvertedIndex struct {
	index    map[string]*PostingList
	docCount uint32
}

// NewInvertedIndex 创建空索引
func NewInvertedIndex() *InvertedIndex {
	return &InvertedIndex{
		index: make(map[string]*PostingList),
	}
}

// Add 向索引添加一个 (docID, term, position)
// 假设文档按 docID 递增顺序添加
func (ii *InvertedIndex) Add(docID uint32, term string, position uint32) {
	pl, ok := ii.index[term]
	if !ok {
		pl = &PostingList{}
		ii.index[term] = pl
	}

	// 同一文档的同一 term，追加位置
	if len(pl.postings) > 0 {
		last := &pl.postings[len(pl.postings)-1]
		if last.DocID == docID {
			last.TermFreq++
			last.Positions = append(last.Positions, position)
			return
		}
	}

	// 新文档的 posting
	pl.postings = append(pl.postings, Posting{
		DocID:     docID,
		TermFreq:  1,
		Positions: []uint32{position},
	})
}

// Get 取某个 term 的 PostingList
func (ii *InvertedIndex) Get(term string) *PostingList {
	return ii.index[term]
}

// DocCount 已索引的文档总数
func (ii *InvertedIndex) DocCount() uint32 {
	return ii.docCount
}

// SetDocCount 由 IndexWriter 调用更新
func (ii *InvertedIndex) SetDocCount(n uint32) {
	ii.docCount = n
}

// Intersect 两个 PostingList 取交集（AND 查询）
// 要求两个列表都按 DocID 升序排列
func Intersect(a, b *PostingList) *PostingList {
	if a == nil || b == nil {
		return nil
	}
	result := &PostingList{}
	i, j := 0, 0
	for i < a.DocFreq() && j < b.DocFreq() {
		pa := a.Get(i)
		pb := b.Get(j)
		if pa.DocID == pb.DocID {
			result.postings = append(result.postings, pa)
			i++
			j++
		} else if pa.DocID < pb.DocID {
			i++
		} else {
			j++
		}
	}
	return result
}

// Union 两个 PostingList 取并集（OR 查询）
func Union(a, b *PostingList) *PostingList {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	result := &PostingList{}
	i, j := 0, 0
	for i < a.DocFreq() && j < b.DocFreq() {
		pa := a.Get(i)
		pb := b.Get(j)
		if pa.DocID == pb.DocID {
			result.postings = append(result.postings, pa)
			i++
			j++
		} else if pa.DocID < pb.DocID {
			result.postings = append(result.postings, pa)
			i++
		} else {
			result.postings = append(result.postings, pb)
			j++
		}
	}
	// 剩余元素
	for i < a.DocFreq() {
		result.postings = append(result.postings, a.Get(i))
		i++
	}
	for j < b.DocFreq() {
		result.postings = append(result.postings, b.Get(j))
		j++
	}
	return result
}
