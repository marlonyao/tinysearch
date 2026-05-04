package search

import (
	"math"
	"sort"

	"lucy/index"
)

// ScoreDoc 一篇匹配文档及其得分
type ScoreDoc struct {
	DocID uint32
	Score float64
}

// TopDocs 查询结果
type TopDocs struct {
	TotalHits int
	ScoreDocs []ScoreDoc
}

// Query 查询接口
type Query interface {
	Execute(s *IndexSearcher) *TopDocs
}

// IndexSearcher 执行查询并评分
type IndexSearcher struct {
	index *index.InvertedIndex
}

// NewIndexSearcher 创建 Searcher
func NewIndexSearcher(idx *index.InvertedIndex) *IndexSearcher {
	return &IndexSearcher{index: idx}
}

// Index 暴露底层索引（BooleanQuery 子查询需要）
func (s *IndexSearcher) Index() *index.InvertedIndex {
	return s.index
}

// --- TermQuery ---

// TermQuery 单个词查询
type TermQuery struct {
	Term string
}

func (q *TermQuery) Execute(s *IndexSearcher) *TopDocs {
	pl := s.index.Get(q.Term)
	if pl == nil {
		return &TopDocs{}
	}

	N := float64(s.index.DocCount())
	df := float64(pl.DocFreq())
	idf := math.Log(N / df)

	var docs []ScoreDoc
	for _, p := range pl.Postings() {
		tf := float64(p.TermFreq)
		score := tf * idf
		docs = append(docs, ScoreDoc{DocID: p.DocID, Score: score})
	}

	// 按分数降序
	sort.Slice(docs, func(i, j int) bool {
		return docs[i].Score > docs[j].Score
	})

	return &TopDocs{
		TotalHits: len(docs),
		ScoreDocs: docs,
	}
}

// --- PhraseQuery ---

// PhraseQuery 短语查询：terms 必须在文档中连续出现
type PhraseQuery struct {
	Terms []string
}

func (q *PhraseQuery) Execute(s *IndexSearcher) *TopDocs {
	if len(q.Terms) == 0 {
		return &TopDocs{}
	}

	// 收集所有 term 的 posting lists
	var allPLs []*index.PostingList
	for _, term := range q.Terms {
		pl := s.index.Get(term)
		if pl == nil {
			return &TopDocs{}
		}
		allPLs = append(allPLs, pl)
	}

	// 从第一个 term 的 posting list 开始遍历
	var docs []ScoreDoc
	for _, firstPosting := range allPLs[0].Postings() {
		docID := firstPosting.DocID

		// 收集该文档中所有 term 的位置列表
		positions := make([][]uint32, len(q.Terms))
		positions[0] = firstPosting.Positions
		matched := true

		for i := 1; i < len(q.Terms); i++ {
			found := false
			for _, p := range allPLs[i].Postings() {
				if p.DocID == docID {
					positions[i] = p.Positions
					found = true
					break
				}
			}
			if !found {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}

		// 验证位置连续性
		if q.matchPhrase(positions) {
			score := float64(len(q.Terms))
			docs = append(docs, ScoreDoc{DocID: docID, Score: score})
		}
	}

	sort.Slice(docs, func(i, j int) bool {
		return docs[i].Score > docs[j].Score
	})

	return &TopDocs{
		TotalHits: len(docs),
		ScoreDocs: docs,
	}
}

// matchPhrase 检查位置列表中是否存在连续序列
func (q *PhraseQuery) matchPhrase(positions [][]uint32) bool {
	for _, p0 := range positions[0] {
		matched := true
		for i := 1; i < len(q.Terms); i++ {
			target := p0 + uint32(i)
			found := false
			for _, pos := range positions[i] {
				if pos == target {
					found = true
					break
				}
			}
			if !found {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// --- BooleanQuery ---

// Occur 布尔子句出现方式
type Occur int

const (
	MUST Occur = iota     // AND：必须匹配
	SHOULD                // OR：可选，影响评分
	MUST_NOT              // NOT：必须不匹配
)

// BooleanClause 布尔子句
type BooleanClause struct {
	Query Query
	Occur Occur
}

// BooleanQuery 组合查询
type BooleanQuery struct {
	Clauses []BooleanClause
}

func (q *BooleanQuery) Execute(s *IndexSearcher) *TopDocs {
	mustCount := 0
	for _, c := range q.Clauses {
		if c.Occur == MUST {
			mustCount++
		}
	}

	// 收集所有子查询结果
	docScores := make(map[uint32]float64)
	docMustHits := make(map[uint32]int)
	mustNotDocs := make(map[uint32]bool) // MUST_NOT 命中的文档需要排除

	for _, c := range q.Clauses {
		result := c.Query.Execute(s)
		for _, sd := range result.ScoreDocs {
			if c.Occur == MUST_NOT {
				mustNotDocs[sd.DocID] = true
				continue
			}
			docScores[sd.DocID] += sd.Score
			if c.Occur == MUST {
				docMustHits[sd.DocID]++
			}
		}
	}

	// 过滤结果
	var docs []ScoreDoc
	for docID, score := range docScores {
		// MUST_NOT 排除
		if mustNotDocs[docID] {
			continue
		}
		// MUST 过滤
		if mustCount > 0 && docMustHits[docID] < mustCount {
			continue
		}
		docs = append(docs, ScoreDoc{DocID: docID, Score: score})
	}

	sort.Slice(docs, func(i, j int) bool {
		return docs[i].Score > docs[j].Score
	})

	return &TopDocs{
		TotalHits: len(docs),
		ScoreDocs: docs,
	}
}
