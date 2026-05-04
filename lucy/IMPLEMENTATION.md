# Lucy 搜索引擎实现说明

> 一个极简的类 Lucene 搜索引擎，从 0 开始用 Go 实现。
> 代码总量约 2600 行，6 个核心模块，全部测试覆盖。

---

## 一、项目定位与选型

**目标**：学习搜索引擎原理，同时构建一个可用的轻量级搜索引擎。  
**语言**：Go（兼顾原理清晰度与工程实用性）。  
**兼容策略**：不直接兼容 Lucene 索引文件格式（过于复杂），但数据结构概念对齐。

---

## 二、实现阶段总览

| 阶段 | 模块 | 核心产出 | 代码量 |
|---|---|---|---|
| 1 | 项目结构 + 文档模型 | `document/` | ~200 行 |
| 2 | 分词器 | `analysis/` | ~130 行 |
| 3 | 倒排索引核心 | `index/`（内存部分） | ~400 行 |
| 4 | 搜索执行引擎 | `search/`（查询执行） | ~240 行 |
| 5 | 查询解析器 | `search/`（parser） | ~150 行 |
| 6 | HTTP 服务 | `server/` | ~160 行 |
| 7 | 磁盘持久化 | `index/`（序列化） | ~200 行 |
| 8 | 修复与验证 | 跨模块联调 | — |

---

## 三、逐阶段详细实现

### 阶段 1：项目结构与文档模型

**需求**：定义搜索引擎处理的基本单元 —— 文档和字段。

**设计**：
- `Document`：包含唯一 `DocID` 和字段列表
- `Field`：包含名称、值、属性（Store / Index / Tokenize）
- `FieldAttribute`：位掩码标志，支持组合：`Store | Index | Tokenize`

**关键决策**：
- 字段区分 `IndexedFields()` 和存储字段，只有标记 `Index` 的字段进入倒排索引
- `Tokenize` 决定字段值是作为整词索引还是分词索引

**测试**：字段属性位运算、文档构建流程。

---

### 阶段 2：分词器（Tokenizer）

**需求**：将文本切分为可索引的词元（Token）。

**实现**：`StandardTokenizer`

**第一版（仅英文）**：
- 按非字母数字字符切分
- ASCII 转小写
- 记录位置偏移（Position / Start / End）

**第二版（支持中文）**：
- 核心问题：中文没有空格，标准分词器把整段汉字当分隔符丢弃
- 解决方案：**一元分词** —— 每个汉字独立成 token
- 实现方式：在 `isLetterOrDigit` 中增加 CJK 汉字范围判断（`0x4e00 ~ 0x9fff`）
- 混合处理：ASCII 单词保持整词，CJK 字符逐字切分

**示例**：
```
"Go语言编程" → ["go", "语", "言", "编", "程"]
"hello world" → ["hello", "world"]
```

**测试**：英文分词、位置偏移、中文一元分词、中英混合。

---

### 阶段 3：倒排索引核心

**需求**：构建内存中的倒排索引数据结构。

**核心数据结构**：

```go
// Posting：一个词在某篇文档中的完整信息
Posting {
    DocID     uint32   // 文档编号
    TermFreq  uint32   // 出现次数
    Positions []uint32 // 每次出现的位置
}

// PostingList：一个 term 对应的所有文档，按 DocID 升序
PostingList {
    postings []Posting
}

// InvertedIndex：内存倒排索引
InvertedIndex {
    index    map[string]*PostingList
    docCount uint32
}
```

**关键操作**：
- `Add(docID, term, position)`：向索引添加词元
  - 同一文档同一词的连续位置自动合并（增加 TermFreq，追加 Positions）
  - 新文档则追加新 Posting
- `Get(term)`：返回 term 的 PostingList
- `Intersect(a, b)`：AND 查询，双指针取交集
- `Union(a, b)`：OR 查询，双指针取并集

**不变式**：所有 PostingList 按 `DocID` 升序排列，保证交集/并集可用线性双指针算法。

**测试**：索引构建、posting 合并、交集、并集、空值处理。

---

### 阶段 4：索引写入器（IndexWriter）

**需求**：将 Document 通过分词后写入倒排索引。

**流程**：
```
Document → 遍历 IndexedFields → 分词 → InvertedIndex.Add(docID, term, pos)
```

**关键实现**：
- 自动分配递增 `DocID`
- 不分词字段（`Tokenize=false`）：整值作为一个 term，位置为 0
- 分词字段：逐 token 加入索引，记录 token 位置

**接口**：
- `AddDocument(doc)`：内存索引
- `Index()`：暴露当前索引（用于查询）
- `DocCount()`：已索引文档数

---

### 阶段 5：搜索执行引擎

**需求**：在倒排索引上执行查询并评分排序。

**查询接口**：
```go
type Query interface {
    Execute(s *IndexSearcher) *TopDocs
}
```

**三种查询实现**：

#### 5.1 TermQuery（单 term 查询）
- 查倒排表 → 计算 TF-IDF 评分
- `score = tf * idf`，其中 `idf = log(N / df)`
- 按 score 降序返回

#### 5.2 PhraseQuery（短语查询）
- 语义：terms 必须在文档中**连续出现**
- 步骤：
  1. 确保所有 term 在文档中都存在
  2. 验证位置连续性：term[i] 的位置 = term[0] 的位置 + i
- 示例：`"hello world"` 要求 `hello@0` 且 `world@1`

#### 5.3 BooleanQuery（布尔组合查询）
- 子句类型：`MUST`（AND）、`SHOULD`（OR）、`MUST_NOT`（NOT）
- 执行逻辑：
  1. 收集所有子查询的 docID 和 score
  2. `MUST_NOT` 的 docID 加入排除集合
  3. `MUST` 的 docID 必须全部命中
  4. `SHOULD` 的 score 累加

**测试**：TermQuery 评分排序、PhraseQuery 连续位置验证、BooleanQuery 的 AND/OR/NOT/MIXED 场景。

---

### 阶段 6：查询解析器（QueryParser）

**需求**：把用户输入字符串转成可执行的 Query 树。

**支持的语法**：
| 输入 | 解析结果 |
|---|---|
| `hello` | `TermQuery` |
| `hello world` | `BooleanQuery(MUST, MUST)`（默认 AND） |
| `hello AND world` | `BooleanQuery(MUST, MUST)` |
| `hello OR world` | `BooleanQuery(SHOULD, SHOULD)` |
| `hello NOT world` | `BooleanQuery(MUST, MUST_NOT)` |
| `"hello world"` | `PhraseQuery` |

**实现要点**：
1. **引号识别**：`"hello world"` 作为整体短语 token
2. **操作符识别**：AND / OR / NOT 不区分大小写
3. **默认 AND**：无操作符的空格分隔词，自动组合为 AND
4. **中文查询支持**：非短语查询也要过 tokenizer，保证查询词与索引词一致
   - `"搜索引擎"`（非短语）→ 分词为 `BooleanQuery("搜" AND "索" AND "引" AND "擎")`
   - `"搜索引擎"`（带引号）→ `PhraseQuery`（保持位置关系）

**测试**：各类语法解析、短语解析、大小写不敏感、空输入。

---

### 阶段 7：HTTP 服务

**需求**：通过 REST API 暴露索引和搜索能力。

**接口设计**：

| 方法 | 路径 | 功能 |
|---|---|---|
| POST | `/index` | 索引文档（JSON 字段定义） |
| GET | `/search?q=...` | 搜索（自动 URL 解码） |
| GET | `/stats` | 索引统计 |

**请求格式**：
```json
POST /index
{
  "fields": {
    "title": {"value": "Go 入门", "store": true, "index": true, "tokenize": true},
    "content": {"value": "Go 是编程语言", "store": true, "index": true, "tokenize": true}
  }
}
```

**响应格式**：
```json
GET /search?q=hello
{
  "total": 2,
  "docs": [
    {"doc_id": 0, "score": 1.8326},
    {"doc_id": 3, "score": 1.8326}
  ]
}
```

**关键实现**：
- 每次 `AddDocument` 后更新 `IndexSearcher` 的索引引用
- 查询参数使用 `url.QueryEscape` 确保中文和特殊字符正确传输

**测试**：HTTP 接口的索引、搜索、统计全流程。

---

### 阶段 8：磁盘持久化

**需求**：将内存索引保存到磁盘，并能加载恢复。

#### 8.1 文件格式设计

```
┌─────────────────────────────────────┐
│ Header (16 bytes)                   │
│   Magic(4) | Version(4) | DocCnt(4)│
│   | TermCnt(4)                      │
├─────────────────────────────────────┤
│ [Term 0] TermEntry (变长)            │
│   TermLen(2) | Term | PostingCnt(4)│
│   | Offset(8, 当前填0)               │
│ [Term 0] Postings (变长)             │
│   DocID(4) | TermFreq(4) | PosCnt(4)│
│   | Positions[]                     │
│ [Term 1] TermEntry + Postings ...   │
│ ...                                  │
└─────────────────────────────────────┘
```

**格式特点**：
- 交错存储：每个 term 的目录条目紧跟其 postings 数据
- LittleEndian 编码
- Magic = `"LUCY"`，Version = 1

#### 8.2 Save 流程
```go
func Save(idx *InvertedIndex, w io.Writer) error
```
1. 收集所有 term
2. 写文件头（docCount, termCount）
3. 对每个 term：写 TermEntry → 紧跟写所有 Postings

#### 8.3 Load 流程
```go
func Load(r io.Reader) (*InvertedIndex, error)
```
1. 读 16 字节文件头（验证 Magic/Version）
2. 循环 termCount 次：
   - 读 TermEntry（跳过 Offset 字段）
   - 读 postingCnt 个 Posting（DocID + TermFreq + Positions）
   - 重建 PostingList
3. 返回完整 InvertedIndex

#### 8.4 IndexWriter 集成
- `Commit(path)`：全量序列化到文件（覆盖写）
- `LoadFromFile(path)`：从文件加载，恢复 `nextDocID`

**测试**：
- 空索引序列化
- 完整 round-trip（保存 → 加载 → 对比）
- 文件加载后搜索功能正常
- Magic 校验（无效文件拒绝）

---

## 四、关键 Bug 修复记录

### Bug 1：序列化格式不一致导致 `unexpected EOF`

**现象**：`Save` 后 `Load` 报错 `read posting: unexpected EOF`

**根因**：
- `Save` 原始实现：先写完所有 TermEntry，再写完所有 Postings（两段式）
- `Load` 实现：读一个 TermEntry → 立即读其 Postings（交错式）
- 两边对不上，读数据错位

**修复**：将 `Save` 改成交错写入，与 `Load` 对齐。

### Bug 2：中文搜索无结果

**现象**：搜索 `"搜索引擎"` 返回 0 个结果

**根因**：
- 索引侧：分词器已支持中文一元分词，索引中有 `"搜"、"索"、"引"、"擎"`
- 查询侧：QueryParser 直接把 `"搜索引擎"` 当做一个完整 term 去查

**修复**：`tokenToQuery` 中非短语查询也过 tokenizer，与索引侧保持一致的 token 粒度。

### Bug 3：HTTP 查询中布尔查询返回 0

**现象**：`"Go AND 并发"`、`"Rust OR Go"` 返回 0

**根因**：URL 参数中的空格未编码，服务端接收到的 q 被截断

**修复**：客户端请求时使用 `url.QueryEscape(q)` 编码查询参数。

---

## 五、验证结果

### 单元测试
```bash
$ cd lucy && go test ./...
ok      lucy/analysis
ok      lucy/document
ok      lucy/index
ok      lucy/search
ok      lucy/server
```

### 短语搜索验证

| 查询 | 结果 | 说明 |
|---|---|---|
| `"hello world"` | doc0, doc3 | 连续出现 ✓ |
| `"hello lucene"` | doc1 | 连续出现 ✓ |
| `"world hello"` | 0 篇 | 顺序不匹配 ✓ |

### NOT 查询验证

| 查询 | 结果 | 说明 |
|---|---|---|
| `hello NOT world` | doc1 | 有 hello 无 world ✓ |
| `lucene NOT hello` | doc2 | 有 lucene 无 hello ✓ |
| `hello AND lucene NOT world` | doc1 | 组合条件 ✓ |

### 中文搜索验证

| 查询 | 结果 | 说明 |
|---|---|---|
| `搜索引擎` | 2 篇 | 一元分词 + AND 组合 ✓ |
| `内存安全` | 1 篇 | 中文词组搜索 ✓ |

---

## 六、目录结构

```
lucy/
├── go.mod
├── analysis/
│   ├── tokenizer.go          # 分词器接口 + StandardTokenizer
│   └── tokenizer_test.go     # 分词测试
├── document/
│   ├── document.go           # Document + Field 模型
│   └── document_test.go      # 文档测试
├── index/
│   ├── index.go              # InvertedIndex + PostingList
│   ├── index_test.go         # 索引操作测试
│   ├── writer.go             # IndexWriter
│   ├── writer_test.go        # 写入器测试
│   ├── serialize.go          # 磁盘序列化 Save/Load
│   └── serialize_test.go     # 持久化测试
├── search/
│   ├── search.go             # Query 接口 + 执行引擎
│   ├── search_test.go        # 搜索测试
│   ├── parser.go             # QueryParser
│   └── parser_test.go        # 解析器测试
├── server/
│   ├── server.go             # HTTP 服务
│   └── server_test.go        # HTTP 接口测试
└── cmd/
    ├── demo/
    │   └── main.go           # HTTP 演示程序
    └── verify/
        └── main.go           # 短语/NOT 查询验证程序
```

---

## 七、下一步可选扩展

| 功能 | 难度 | 说明 |
|---|---|---|
| 增量索引（Segment） | 中 | 多段追加 + 段合并策略 |
| mmap 加载 | 中 | 不改文件格式，改 `InvertedIndex.Get` 为 lazy 解码 |
| BM25 评分 | 低 | 替换 TF-IDF，业界更常用 |
| 字段搜索 | 低 | `title:hello` 限定字段查询 |
| 删除文档 | 中 | 标记删除 + 合并时清理 |
| 高亮结果 | 中 | 记录原始文本位置，返回片段 |

---

*文档版本：v1.0*  
*对应代码：lucy_search_engine.zip*
