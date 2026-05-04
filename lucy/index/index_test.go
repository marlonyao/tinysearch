package index

import (
	"reflect"
	"testing"
)

func TestInvertedIndexAddAndGet(t *testing.T) {
	ii := NewInvertedIndex()

	// doc 0: "hello" at position 0
	ii.Add(0, "hello", 0)
	// doc 0: "world" at position 1
	ii.Add(0, "world", 1)
	// doc 1: "hello" at position 0
	ii.Add(1, "hello", 0)
	// doc 2: "world" at position 0
	ii.Add(2, "world", 0)

	// 查 "hello"
	pl := ii.Get("hello")
	if pl.DocFreq() != 2 {
		t.Fatalf("hello DocFreq = %d, want 2", pl.DocFreq())
	}
	p0 := pl.Get(0)
	if p0.DocID != 0 || p0.TermFreq != 1 || len(p0.Positions) != 1 || p0.Positions[0] != 0 {
		t.Errorf("hello[0] = %+v, want DocID=0 TermFreq=1 Positions=[0]", p0)
	}
	p1 := pl.Get(1)
	if p1.DocID != 1 || p1.TermFreq != 1 {
		t.Errorf("hello[1] = %+v, want DocID=1 TermFreq=1", p1)
	}

	// 查 "world"
	plWorld := ii.Get("world")
	if plWorld.DocFreq() != 2 {
		t.Fatalf("world DocFreq = %d, want 2", plWorld.DocFreq())
	}

	// 查不存在的词
	if ii.Get("notexist") != nil {
		t.Error("expected nil for non-existent term")
	}
}

func TestInvertedIndexTermFreqAndPositions(t *testing.T) {
	ii := NewInvertedIndex()

	// doc 0: "go" 出现 3 次
	ii.Add(0, "go", 0)
	ii.Add(0, "go", 5)
	ii.Add(0, "go", 10)

	// doc 1: "go" 出现 1 次
	ii.Add(1, "go", 2)

	pl := ii.Get("go")
	if pl.DocFreq() != 2 {
		t.Fatalf("go DocFreq = %d, want 2", pl.DocFreq())
	}

	p0 := pl.Get(0)
	if p0.TermFreq != 3 {
		t.Errorf("doc0 TermFreq = %d, want 3", p0.TermFreq)
	}
	wantPositions := []uint32{0, 5, 10}
	if !reflect.DeepEqual(p0.Positions, wantPositions) {
		t.Errorf("doc0 Positions = %v, want %v", p0.Positions, wantPositions)
	}

	p1 := pl.Get(1)
	if p1.TermFreq != 1 || p1.Positions[0] != 2 {
		t.Errorf("doc1 = %+v, want TermFreq=1 Positions=[2]", p1)
	}
}

func TestIntersect(t *testing.T) {
	ii := NewInvertedIndex()
	// doc 0: hello, world
	ii.Add(0, "hello", 0)
	ii.Add(0, "world", 1)
	// doc 1: hello
	ii.Add(1, "hello", 0)
	// doc 2: world
	ii.Add(2, "world", 0)
	// doc 3: hello, world
	ii.Add(3, "hello", 0)
	ii.Add(3, "world", 1)

	helloPL := ii.Get("hello")
	worldPL := ii.Get("world")

	result := Intersect(helloPL, worldPL)
	if result.DocFreq() != 2 {
		t.Fatalf("intersect DocFreq = %d, want 2", result.DocFreq())
	}
	ids := []uint32{result.Get(0).DocID, result.Get(1).DocID}
	want := []uint32{0, 3}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("intersect result = %v, want %v", ids, want)
	}

	// nil 交集
	if Intersect(nil, helloPL) != nil {
		t.Error("intersect with nil should return nil")
	}
}

func TestUnion(t *testing.T) {
	ii := NewInvertedIndex()
	// doc 0: hello
	ii.Add(0, "hello", 0)
	// doc 1: world
	ii.Add(1, "world", 0)
	// doc 2: hello, world
	ii.Add(2, "hello", 0)
	ii.Add(2, "world", 1)
	// doc 3: hello
	ii.Add(3, "hello", 0)

	helloPL := ii.Get("hello") // docs: 0, 2, 3
	worldPL := ii.Get("world") // docs: 1, 2

	result := Union(helloPL, worldPL)
	if result.DocFreq() != 4 {
		t.Fatalf("union DocFreq = %d, want 4", result.DocFreq())
	}
	ids := make([]uint32, result.DocFreq())
	for i := range ids {
		ids[i] = result.Get(i).DocID
	}
	want := []uint32{0, 1, 2, 3}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("union result = %v, want %v", ids, want)
	}

	// nil 并集
	unionNil := Union(nil, helloPL)
	if unionNil.DocFreq() != helloPL.DocFreq() {
		t.Error("union with nil should return non-nil list")
	}
}

func TestDocCount(t *testing.T) {
	ii := NewInvertedIndex()
	ii.SetDocCount(100)
	if ii.DocCount() != 100 {
		t.Errorf("DocCount = %d, want 100", ii.DocCount())
	}
}
