package document

// FieldAttribute 定义字段的索引属性
type FieldAttribute int

const (
	// Store 字段原始值会被保存到索引中，搜索结果可以返回该值
	Store FieldAttribute = 1 << iota
	// Index 字段会被建立倒排索引，可被搜索
	Index
	// Tokenize 字段内容会被分词（只对 Index 字段有意义）
	Tokenize
)

// Field 表示文档中的一个字段
type Field struct {
	Name  string // 字段名，如 "title", "content"
	Value string // 字段原始值
	Attr  FieldAttribute
}

// NewField 创建字段，默认仅存储（不参与搜索）
func NewField(name, value string, attr FieldAttribute) Field {
	return Field{
		Name:  name,
		Value: value,
		Attr:  attr,
	}
}

// IsStored 是否保存原始值
func (f Field) IsStored() bool {
	return f.Attr&Store != 0
}

// IsIndexed 是否建立倒排索引
func (f Field) IsIndexed() bool {
	return f.Attr&Index != 0
}

// IsTokenized 是否分词
func (f Field) IsTokenized() bool {
	return f.Attr&Tokenize != 0
}

// Document 表示一个待索引的文档
type Document struct {
	ID     uint32  // 文档唯一标识，由 IndexWriter 分配
	Fields []Field // 文档包含的所有字段
}

// NewDocument 创建空文档
func NewDocument() *Document {
	return &Document{
		Fields: make([]Field, 0),
	}
}

// Add 向文档添加字段
func (d *Document) Add(fields ...Field) {
	d.Fields = append(d.Fields, fields...)
}

// Get 按名称获取第一个匹配的字段
func (d *Document) Get(name string) (Field, bool) {
	for _, f := range d.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// IndexedFields 返回所有需要建立倒排索引的字段
func (d *Document) IndexedFields() []Field {
	var result []Field
	for _, f := range d.Fields {
		if f.IsIndexed() {
			result = append(result, f)
		}
	}
	return result
}

// StoredFields 返回所有需要保存原始值的字段
func (d *Document) StoredFields() []Field {
	var result []Field
	for _, f := range d.Fields {
		if f.IsStored() {
			result = append(result, f)
		}
	}
	return result
}
