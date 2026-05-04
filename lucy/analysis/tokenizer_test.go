package analysis

import (
	"reflect"
	"testing"
)

func TestStandardTokenizer(t *testing.T) {
	tests := []struct {
		input string
		want  []string // 只比对 term
	}{
		{"Hello World", []string{"hello", "world"}},
		{"Go-Search, Engine! v1.0", []string{"go", "search", "engine", "v1", "0"}},
		{"UPPER lower MiXeD", []string{"upper", "lower", "mixed"}},
		{"123abc", []string{"123abc"}},
		{"   spaces   ", []string{"spaces"}},
		{"", nil},
		{"!@#$%", nil},
	}

	tok := StandardTokenizer{}
	for _, tt := range tests {
		tokens := tok.Tokenize(tt.input)
		var got []string
		for _, tk := range tokens {
			got = append(got, tk.Term)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Tokenize(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestTokenPositions(t *testing.T) {
	tok := StandardTokenizer{}
	tokens := tok.Tokenize("Hello Lucene")

	if len(tokens) != 2 {
		t.Fatalf("len(tokens) = %d, want 2", len(tokens))
	}

	// 注意：按 rune 位置，"Hello" = 5 runes，空格 1，"Lucene" = 6
	if tokens[0].Position != 0 {
		t.Errorf("token[0].Position = %d, want 0", tokens[0].Position)
	}
	if tokens[0].Start != 0 || tokens[0].End != 5 {
		t.Errorf("token[0] offsets = %d:%d, want 0:5", tokens[0].Start, tokens[0].End)
	}

	if tokens[1].Position != 1 {
		t.Errorf("token[1].Position = %d, want 1", tokens[1].Position)
	}
	if tokens[1].Start != 6 || tokens[1].End != 12 {
		t.Errorf("token[1] offsets = %d:%d, want 6:12", tokens[1].Start, tokens[1].End)
	}
}

func TestChineseTokenization(t *testing.T) {
	// 中文一元分词：每个汉字一个 token
	tok := StandardTokenizer{}
	tokens := tok.Tokenize("你好世界")
	want := []string{"你", "好", "世", "界"}
	var got []string
	for _, tk := range tokens {
		got = append(got, tk.Term)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize(你好世界) = %v, want %v", got, want)
	}
}

func TestMixedChineseEnglish(t *testing.T) {
	tok := StandardTokenizer{}
	tokens := tok.Tokenize("Go语言编程")
	want := []string{"go", "语", "言", "编", "程"}
	var got []string
	for _, tk := range tokens {
		got = append(got, tk.Term)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize(Go语言编程) = %v, want %v", got, want)
	}
}
