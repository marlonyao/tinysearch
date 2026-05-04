package document

import (
	"testing"
)

func TestNewField(t *testing.T) {
	f := NewField("title", "Hello Lucene", Store|Index|Tokenize)
	if f.Name != "title" {
		t.Errorf("Name = %q, want title", f.Name)
	}
	if f.Value != "Hello Lucene" {
		t.Errorf("Value = %q, want Hello Lucene", f.Value)
	}
	if !f.IsStored() {
		t.Error("expected IsStored() = true")
	}
	if !f.IsIndexed() {
		t.Error("expected IsIndexed() = true")
	}
	if !f.IsTokenized() {
		t.Error("expected IsTokenized() = true")
	}
}

func TestFieldAttributes(t *testing.T) {
	tests := []struct {
		attr              FieldAttribute
		wantStored        bool
		wantIndexed       bool
		wantTokenized     bool
	}{
		{Store, true, false, false},
		{Index, false, true, false},
		{Tokenize, false, false, true},
		{Store | Index, true, true, false},
		{Store | Index | Tokenize, true, true, true},
		{0, false, false, false},
	}

	for _, tt := range tests {
		f := NewField("x", "y", tt.attr)
		if f.IsStored() != tt.wantStored {
			t.Errorf("attr=%d: IsStored() = %v, want %v", tt.attr, f.IsStored(), tt.wantStored)
		}
		if f.IsIndexed() != tt.wantIndexed {
			t.Errorf("attr=%d: IsIndexed() = %v, want %v", tt.attr, f.IsIndexed(), tt.wantIndexed)
		}
		if f.IsTokenized() != tt.wantTokenized {
			t.Errorf("attr=%d: IsTokenized() = %v, want %v", tt.attr, f.IsTokenized(), tt.wantTokenized)
		}
	}
}

func TestDocumentAddAndGet(t *testing.T) {
	doc := NewDocument()
	doc.Add(
		NewField("title", "Go Search Engine", Store|Index|Tokenize),
		NewField("url", "https://example.com", Store),
	)

	if len(doc.Fields) != 2 {
		t.Fatalf("len(Fields) = %d, want 2", len(doc.Fields))
	}

	title, ok := doc.Get("title")
	if !ok {
		t.Fatal("expected to find field 'title'")
	}
	if title.Value != "Go Search Engine" {
		t.Errorf("title.Value = %q, want Go Search Engine", title.Value)
	}

	url, ok := doc.Get("url")
	if !ok {
		t.Fatal("expected to find field 'url'")
	}
	if url.IsIndexed() {
		t.Error("url should not be indexed")
	}
	if !url.IsStored() {
		t.Error("url should be stored")
	}

	_, ok = doc.Get("notexist")
	if ok {
		t.Error("expected not to find field 'notexist'")
	}
}

func TestDocumentIndexedFields(t *testing.T) {
	doc := NewDocument()
	doc.Add(
		NewField("title", "abc", Store|Index|Tokenize),
		NewField("url", "http://x.com", Store),
		NewField("content", "xyz", Store|Index|Tokenize),
	)

	indexed := doc.IndexedFields()
	if len(indexed) != 2 {
		t.Fatalf("len(IndexedFields) = %d, want 2", len(indexed))
	}

	names := []string{indexed[0].Name, indexed[1].Name}
	if names[0] != "title" || names[1] != "content" {
		t.Errorf("got names %v, want [title content]", names)
	}
}

func TestDocumentStoredFields(t *testing.T) {
	doc := NewDocument()
	doc.Add(
		NewField("title", "abc", Store|Index|Tokenize),
		NewField("internal_id", "42", Index),
	)

	stored := doc.StoredFields()
	if len(stored) != 1 {
		t.Fatalf("len(StoredFields) = %d, want 1", len(stored))
	}
	if stored[0].Name != "title" {
		t.Errorf("stored[0].Name = %q, want title", stored[0].Name)
	}
}
