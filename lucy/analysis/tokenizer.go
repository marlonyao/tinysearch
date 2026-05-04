package analysis

// Token 表示一个切分后的词元
type Token struct {
	Term     string // 标准化后的词（如小写、词干化后）
	Position int    // 在原文中的位置序号，从 0 开始
	Start    int    // 在原文中的起始字节偏移
	End      int    // 在原文中的结束字节偏移
}

// Tokenizer 分词器接口
type Tokenizer interface {
	Tokenize(text string) []Token
}

// StandardTokenizer 按非字母数字切分，并转为小写
type StandardTokenizer struct{}

func (StandardTokenizer) Tokenize(text string) []Token {
	var tokens []Token
	runes := []rune(text)
	start := -1
	isAsciiWord := false

	for i, r := range runes {
		if isAsciiLetterOrDigit(r) {
			if !isAsciiWord {
				// 结束之前的中文或空白
				start = i
				isAsciiWord = true
			}
			// 延续 ASCII 单词
		} else if isCJK(r) {
			// 结束之前的 ASCII 单词
			if isAsciiWord && start != -1 {
				tokens = append(tokens, makeAsciiToken(runes, start, i, len(tokens)))
				isAsciiWord = false
			}
			// 每个 CJK 字符独立成 token
			tokens = append(tokens, Token{
				Term:     string(r),
				Position: len(tokens),
				Start:    i,
				End:      i + 1,
			})
			start = -1
		} else {
			// 分隔符：结束 ASCII 单词
			if isAsciiWord && start != -1 {
				tokens = append(tokens, makeAsciiToken(runes, start, i, len(tokens)))
				isAsciiWord = false
			}
			start = -1
		}
	}
	if isAsciiWord && start != -1 {
		tokens = append(tokens, makeAsciiToken(runes, start, len(runes), len(tokens)))
	}
	return tokens
}

func isAsciiLetterOrDigit(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

func isCJK(r rune) bool {
	return r >= 0x4e00 && r <= 0x9fff
}

func makeAsciiToken(runes []rune, start, end, pos int) Token {
	term := string(runes[start:end])
	// 转为小写
	lower := []rune(term)
	for i, r := range lower {
		if r >= 'A' && r <= 'Z' {
			lower[i] = r + ('a' - 'A')
		}
	}
	return Token{
		Term:     string(lower),
		Position: pos,
		Start:    start,
		End:      end,
	}
}
