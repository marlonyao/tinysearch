package search

import "testing"

func TestQueryParserNot(t *testing.T) {
	p := NewQueryParser()
	q := p.Parse("hello NOT world")

	bq, ok := q.(*BooleanQuery)
	if !ok {
		t.Fatalf("expected BooleanQuery, got %T", q)
	}
	if len(bq.Clauses) != 2 {
		t.Fatalf("len(Clauses) = %d, want 2", len(bq.Clauses))
	}
	if bq.Clauses[0].Occur != MUST {
		t.Errorf("clause[0] Occur = %v, want MUST", bq.Clauses[0].Occur)
	}
	if bq.Clauses[1].Occur != MUST_NOT {
		t.Errorf("clause[1] Occur = %v, want MUST_NOT", bq.Clauses[1].Occur)
	}
}

func TestQueryParserNotEndToEnd(t *testing.T) {
	idx := buildIndex([][]string{
		{"hello world"},
		{"hello lucene"},
		{"world lucene"},
	})
	searcher := NewIndexSearcher(idx)
	parser := NewQueryParser()

	// hello NOT world → doc1 唯一
	q := parser.Parse("hello NOT world")
	result := q.Execute(searcher)
	if result.TotalHits != 1 || result.ScoreDocs[0].DocID != 1 {
		t.Errorf("NOT query failed: %v", result.ScoreDocs)
	}

	// lucene NOT hello → doc2 唯一
	q = parser.Parse("lucene NOT hello")
	result = q.Execute(searcher)
	if result.TotalHits != 1 || result.ScoreDocs[0].DocID != 2 {
		t.Errorf("NOT query failed: %v", result.ScoreDocs)
	}
}
