package search

import (
	"strings"
	"unicode"

	"lucy/analysis"
)

// QueryParser 将用户输入字符串解析为 Query
type QueryParser struct {
	tokenizer analysis.Tokenizer
}

// NewQueryParser 创建查询解析器
func NewQueryParser() *QueryParser {
	return &QueryParser{
		tokenizer: analysis.StandardTokenizer{},
	}
}

// Parse 解析查询字符串
// 语法：
//   term                → TermQuery
//   "term1 term2"       → PhraseQuery
//   term1 term2         → BooleanQuery(MUST, MUST)   默认 AND
//   term1 AND term2     → BooleanQuery(MUST, MUST)
//   term1 OR term2      → BooleanQuery(SHOULD, SHOULD)
func (p *QueryParser) Parse(input string) Query {
	tokens := tokenizeQuery(input)
	if len(tokens) == 0 {
		return &TermQuery{Term: ""}
	}
	return p.buildQuery(tokens)
}

// tokenizeQuery 把查询字符串切分为词元和操作符
// 支持引号包裹的短语："hello world" → 一个短语 token
type queryToken struct {
	Text     string
	IsPhrase bool
}

func tokenizeQuery(input string) []queryToken {
	var result []queryToken
	var current strings.Builder
	inQuote := false

	for _, r := range input {
		if r == '"' {
			if inQuote {
				// 结束引号：当前内容作为短语
				if current.Len() > 0 {
					result = append(result, queryToken{Text: current.String(), IsPhrase: true})
					current.Reset()
				}
				inQuote = false
			} else {
				// 开始引号：先把当前内容flush
				if current.Len() > 0 {
					result = append(result, queryToken{Text: current.String(), IsPhrase: false})
					current.Reset()
				}
				inQuote = true
			}
			continue
		}

		if !inQuote && unicode.IsSpace(r) {
			if current.Len() > 0 {
				result = append(result, queryToken{Text: current.String(), IsPhrase: false})
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
	}

	// 处理末尾残留
	if current.Len() > 0 {
		result = append(result, queryToken{Text: current.String(), IsPhrase: inQuote})
	}

	return result
}

// buildQuery 从 token 列表构建 Query
func (p *QueryParser) buildQuery(tokens []queryToken) Query {
	if len(tokens) == 1 {
		return p.tokenToQuery(tokens[0])
	}

	// 扫描 AND/OR 操作符
	for i, tok := range tokens {
		if tok.IsPhrase {
			continue // 短语内部的操作符不识别
		}
		upper := strings.ToUpper(tok.Text)
		if upper == "AND" {
			left := p.buildQuery(tokens[:i])
			right := p.buildQuery(tokens[i+1:])
			return &BooleanQuery{
				Clauses: []BooleanClause{
					{Query: left, Occur: MUST},
					{Query: right, Occur: MUST},
				},
			}
		}
		if upper == "OR" {
			left := p.buildQuery(tokens[:i])
			right := p.buildQuery(tokens[i+1:])
			return &BooleanQuery{
				Clauses: []BooleanClause{
					{Query: left, Occur: SHOULD},
					{Query: right, Occur: SHOULD},
				},
			}
		}
		if upper == "NOT" && i > 0 {
			// NOT 在开头不处理，当成普通词
			left := p.buildQuery(tokens[:i])
			right := p.buildQuery(tokens[i+1:])
			return &BooleanQuery{
				Clauses: []BooleanClause{
					{Query: left, Occur: MUST},
					{Query: right, Occur: MUST_NOT},
				},
			}
		}
	}

	// 没有显式操作符，默认 AND
	var clauses []BooleanClause
	for _, tok := range tokens {
		q := p.tokenToQuery(tok)
		clauses = append(clauses, BooleanClause{Query: q, Occur: MUST})
	}
	return &BooleanQuery{Clauses: clauses}
}

// tokenToQuery 单个 token 转 Query
// 普通查询词也会过 tokenizer：中文一元分词后默认 AND 组合
func (p *QueryParser) tokenToQuery(tok queryToken) Query {
	if tok.IsPhrase {
		// 短语：分词后创建 PhraseQuery（保持位置关系）
		tokens := p.tokenizer.Tokenize(tok.Text)
		var terms []string
		for _, t := range tokens {
			terms = append(terms, t.Term)
		}
		return &PhraseQuery{Terms: terms}
	}

	// 普通词：过 tokenizer（支持中文一元分词）
	tokens := p.tokenizer.Tokenize(tok.Text)
	if len(tokens) == 0 {
		return &TermQuery{Term: ""}
	}
	if len(tokens) == 1 {
		return &TermQuery{Term: tokens[0].Term}
	}

	// 多个 term 默认 AND 组合
	var clauses []BooleanClause
	for _, t := range tokens {
		clauses = append(clauses, BooleanClause{
			Query: &TermQuery{Term: t.Term},
			Occur: MUST,
		})
	}
	return &BooleanQuery{Clauses: clauses}
}
