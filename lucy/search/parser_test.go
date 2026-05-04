package search

import (
	"testing"
)

func TestQueryParserSingleTerm(t *testing.T) {
	p := NewQueryParser()
	q := p.Parse("Hello")

	tq, ok := q.(*TermQuery)
	if !ok {
		t.Fatalf("expected TermQuery, got %T", q)
	}
	if tq.Term != "hello" {
		t.Errorf("Term = %q, want hello", tq.Term)
	}
}

func TestQueryParserDefaultAnd(t *testing.T) {
	p := NewQueryParser()
	q := p.Parse("hello world")

	bq, ok := q.(*BooleanQuery)
	if !ok {
		t.Fatalf("expected BooleanQuery, got %T", q)
	}
	if len(bq.Clauses) != 2 {
		t.Fatalf("len(Clauses) = %d, want 2", len(bq.Clauses))
	}
	for _, c := range bq.Clauses {
		if c.Occur != MUST {
			t.Errorf("expected MUST, got %v", c.Occur)
		}
	}

	_, ok1 := bq.Clauses[0].Query.(*TermQuery)
	_, ok2 := bq.Clauses[1].Query.(*TermQuery)
	if !ok1 || !ok2 {
		t.Error("expected TermQuery children")
	}
}

func TestQueryParserExplicitAnd(t *testing.T) {
	p := NewQueryParser()
	q := p.Parse("hello AND world")

	bq, ok := q.(*BooleanQuery)
	if !ok {
		t.Fatalf("expected BooleanQuery, got %T", q)
	}
	if len(bq.Clauses) != 2 {
		t.Fatalf("len(Clauses) = %d, want 2", len(bq.Clauses))
	}
	for _, c := range bq.Clauses {
		if c.Occur != MUST {
			t.Errorf("expected MUST, got %v", c.Occur)
		}
	}
}

func TestQueryParserExplicitOr(t *testing.T) {
	p := NewQueryParser()
	q := p.Parse("hello OR world")

	bq, ok := q.(*BooleanQuery)
	if !ok {
		t.Fatalf("expected BooleanQuery, got %T", q)
	}
	if len(bq.Clauses) != 2 {
		t.Fatalf("len(Clauses) = %d, want 2", len(bq.Clauses))
	}
	for _, c := range bq.Clauses {
		if c.Occur != SHOULD {
			t.Errorf("expected SHOULD, got %v", c.Occur)
		}
	}
}

func TestQueryParserMultipleTermsDefaultAnd(t *testing.T) {
	p := NewQueryParser()
	q := p.Parse("go search engine")

	bq, ok := q.(*BooleanQuery)
	if !ok {
		t.Fatalf("expected BooleanQuery, got %T", q)
	}
	if len(bq.Clauses) != 3 {
		t.Fatalf("len(Clauses) = %d, want 3", len(bq.Clauses))
	}
	terms := []string{
		bq.Clauses[0].Query.(*TermQuery).Term,
		bq.Clauses[1].Query.(*TermQuery).Term,
		bq.Clauses[2].Query.(*TermQuery).Term,
	}
	want := []string{"go", "search", "engine"}
	for i, w := range want {
		if terms[i] != w {
			t.Errorf("clause[%d] term = %q, want %q", i, terms[i], w)
		}
	}
}

func TestQueryParserCaseInsensitive(t *testing.T) {
	p := NewQueryParser()
	q := p.Parse("Hello WORLD")

	bq := q.(*BooleanQuery)
	if bq.Clauses[0].Query.(*TermQuery).Term != "hello" {
		t.Errorf("expected lowercase term")
	}
	if bq.Clauses[1].Query.(*TermQuery).Term != "world" {
		t.Errorf("expected lowercase term")
	}
}

func TestQueryParserEmpty(t *testing.T) {
	p := NewQueryParser()
	q := p.Parse("")

	_, ok := q.(*TermQuery)
	if !ok {
		t.Fatalf("expected TermQuery for empty input, got %T", q)
	}
}

func TestQueryParserPhrase(t *testing.T) {
	p := NewQueryParser()
	q := p.Parse(`"hello world"`)

	pq, ok := q.(*PhraseQuery)
	if !ok {
		t.Fatalf("expected PhraseQuery, got %T", q)
	}
	if len(pq.Terms) != 2 {
		t.Fatalf("len(Terms) = %d, want 2", len(pq.Terms))
	}
	if pq.Terms[0] != "hello" || pq.Terms[1] != "world" {
		t.Errorf("Terms = %v, want [hello world]", pq.Terms)
	}
}

func TestQueryParserPhraseSingleTerm(t *testing.T) {
	p := NewQueryParser()
	q := p.Parse(`"hello"`)

	pq, ok := q.(*PhraseQuery)
	if !ok {
		t.Fatalf("expected PhraseQuery, got %T", q)
	}
	if len(pq.Terms) != 1 || pq.Terms[0] != "hello" {
		t.Errorf("unexpected Terms: %v", pq.Terms)
	}
}

func TestQueryParserPhraseWithBoolean(t *testing.T) {
	p := NewQueryParser()
	q := p.Parse(`hello AND "world lucene"`)

	bq, ok := q.(*BooleanQuery)
	if !ok {
		t.Fatalf("expected BooleanQuery, got %T", q)
	}
	if len(bq.Clauses) != 2 {
		t.Fatalf("len(Clauses) = %d, want 2", len(bq.Clauses))
	}

	_, ok1 := bq.Clauses[0].Query.(*TermQuery)
	if !ok1 {
		t.Error("expected first clause to be TermQuery")
	}

	pq, ok2 := bq.Clauses[1].Query.(*PhraseQuery)
	if !ok2 {
		t.Fatalf("expected second clause to be PhraseQuery, got %T", bq.Clauses[1].Query)
	}
	if len(pq.Terms) != 2 || pq.Terms[0] != "world" || pq.Terms[1] != "lucene" {
		t.Errorf("phrase terms = %v, want [world lucene]", pq.Terms)
	}
}

func TestQueryParserUnclosedQuote(t *testing.T) {
	p := NewQueryParser()
	q := p.Parse(`"hello world`)

	pq, ok := q.(*PhraseQuery)
	if !ok {
		t.Fatalf("expected PhraseQuery for unclosed quote, got %T", q)
	}
	if len(pq.Terms) != 2 {
		t.Errorf("expected 2 terms, got %d", len(pq.Terms))
	}
}

func TestQueryParserEndToEnd(t *testing.T) {
	idx := buildIndex([][]string{
		{"hello world"},
		{"hello lucene"},
		{"world lucene"},
	})
	searcher := NewIndexSearcher(idx)
	parser := NewQueryParser()

	// AND
	q := parser.Parse("hello world")
	result := q.Execute(searcher)
	if result.TotalHits != 1 || result.ScoreDocs[0].DocID != 0 {
		t.Errorf("AND query failed: %v", result.ScoreDocs)
	}

	// OR
	q = parser.Parse("hello OR world")
	result = q.Execute(searcher)
	if result.TotalHits != 3 {
		t.Errorf("OR query failed: expected 3, got %d", result.TotalHits)
	}

	// 短语
	q = parser.Parse(`"hello world"`)
	result = q.Execute(searcher)
	if result.TotalHits != 1 || result.ScoreDocs[0].DocID != 0 {
		t.Errorf("Phrase query failed: %v", result.ScoreDocs)
	}

	// 短语不匹配
	q = parser.Parse(`"hello lucene"`)
	result = q.Execute(searcher)
	if result.TotalHits != 1 || result.ScoreDocs[0].DocID != 1 {
		t.Errorf("Phrase query 'hello lucene' failed: %v", result.ScoreDocs)
	}
}
