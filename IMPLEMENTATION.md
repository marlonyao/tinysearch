# TinyES 实现说明文档

> 一个极简 Elasticsearch-like 分布式搜索引擎的 Go 实现。
> 从零开始，TDD 驱动，逐步构建。

---

## 一、项目概述

**TinyES** 是一个学习性质的分布式搜索引擎，实现了 ES 核心机制的简化版本：

- **分片（Shard）**：索引数据水平分片存储
- **副本（Replica）**：主从复制保证可用性
- **分布式查询**：跨分片并行搜索，结果聚合
- **HTTP REST API**：兼容 ES 风格的接口
- **多节点集群**：支持节点发现与加入

---

## 二、核心特性实现顺序

### Phase 1: 倒排索引引擎（复用 Lucy）
- 基于之前实现的 `lucy` 倒排索引
- 支持 Term Query、Boolean Query、Phrase Query
- TF-IDF 评分

### Phase 2: 分片管理
- **ShardEngine**：包装单个倒排索引，作为分片的存储引擎
- **Shard**：分片元数据（ID、状态、所属节点）
- **ShardManager**：分片生命周期管理（分配、启动、关闭）
- **路由算法**：`shard = hash(docID) % numShards`

### Phase 3: 集群状态与协调
- **ClusterState**：集群全局状态（节点列表、索引配置、分片分配表）
- **Node**：节点核心，管理本地分片，协调集群操作
- **Transport**：节点间通信层（HTTP 风格）
- **Master 选举**：简单固定 Master 模式

### Phase 4: 分布式索引
- 文档路由到目标分片
- Primary-Replica 复制流程
- 分配时错开 Primary 和 Replica：`node = (shardID + replicaIdx) % nodeCount`

### Phase 5: 分布式搜索
- **SearchRequest/SearchResponse**：跨节点搜索消息
- 并行查询所有相关分片（Primary 或 Replica）
- Top-K 截断 + 分数排序
- Replica 负载均衡与故障降级

### Phase 6: HTTP REST API
- `PUT /:index` — 创建索引
- `POST /:index/_doc/:id` — 索引文档
- `GET /:index/_search?q=query` — 分布式搜索
- `GET /_cluster/health` — 健康检查
- `GET /_cluster/state` — 集群状态

### Phase 7: 多节点启动入口
- `cmd/es/main.go` 支持 `-node`、`-http`、`-join` 参数
- 可启动多节点集群

---

## 三、数据结构地图

### ClusterState（集群状态）
```
ClusterState
├── Version          int64       // 状态版本号，单调递增
├── Nodes            []NodeInfo  // 集群节点列表
├── Indices          map[string]IndexConfig  // 索引名 → 配置
│   └── IndexConfig
│       ├── NumShards    int
│       ├── NumReplicas  int
│       └── Settings     map[string]string
└── RoutingTable       map[string][]ShardAllocation  // 索引名 → 分片分配表
    └── ShardAllocation
        ├── ShardID      int
        ├── NodeID       string      // Primary 所在节点
        ├── State        string      // UNASSIGNED/INITIALIZING/STARTED
        └── Replicas     []ReplicaAllocation
            └── ReplicaAllocation
                ├── NodeID   string
                └── State    string
```

### Node（节点）
```
Node
├── ID               string
├── Address          string
├── IsMaster         bool
├── State          NodeState
├── Shards         map[int]*ShardEngine  // 本地分片
├── ClusterState   *ClusterState
├── Transport      Transport
└── mutex          sync.RWMutex
```

### ShardEngine（分片存储引擎）
```
ShardEngine
├── Index        *lucy.Index  // 倒排索引
├── ShardID      int
├── IsPrimary    bool
├── mutex        sync.RWMutex
└── Search()     // 本地搜索入口
```

### SearchRequest / SearchResponse
```
SearchRequest
├── Index        string
├── Query        string
├── From         int
├── Size         int

SearchResponse
├── Hits         []Hit
├── Total        int
└── Took         int64

Hit
├── DocID        string
├── Score        float64
└── Source       map[string]interface{}
```

---

## 四、关键流程

### 1. 创建索引流程
```
Client → PUT /products
  → Node.CreateIndex("products", shards=3, replicas=1)
    → 生成 ClusterState 更新
    → 分配 Primary 到各节点：(shard+i)%nodeCount
    → 分配 Replica 到不同节点：(shard+i+1)%nodeCount
    → 广播新 ClusterState 到所有节点
    → 各节点启动本地分配的 Shard
```

### 2. 索引文档流程
```
Client → POST /products/_doc/abc123
  → Node.IndexDocument("products", "abc123", doc)
    → routing = hash("abc123") % 3 → shard 1
    → 查找 shard 1 的 Primary 所在节点 → node-1
    → 转发到 node-1（如果是本地直接执行）
    → ShardEngine.Index("abc123", doc)
      → lucy.Index.AddDocument(doc)
    → 同步复制到 Replica 节点
      → Transport.Replicate(node-2, shard1, doc)
      → node-2.ShardEngine.Index("abc123", doc)
```

### 3. 分布式搜索流程
```
Client → GET /products/_search?q=iphone
  → Node.Search("products", "iphone")
    → 从 ClusterState 获取 products 的所有分片分配
    → 对每个分片：
      - 优先查 Primary，或负载均衡选 Replica
      - 构造 SearchRequest，并行发送到各节点
    → 各节点 ShardEngine.Search()
      → lucy.Index.Search(query)
      → 返回本地 Top-K
    → 聚合所有分片结果
      → 按 Score 排序
      → 全局 Top-K 截断
    → 返回 SearchResponse
```

### 4. 节点加入流程
```
NewNode → 启动时指定 -join=existing-node
  → 向现有节点发送 Join 请求
    → Master 验证，更新 ClusterState
    → 分配部分分片到新节点（Rebalance）
    → 广播新状态
  → NewNode 接收 ClusterState
    → 启动分配的 Shard（从 Primary 复制数据）
```

---

## 五、轮询分配算法详解

分片分配采用轮询 + 错开策略：

```go
// Primary 分配
primaryNode = (shardID + 0) % nodeCount

// Replica 分配，错开 Primary
for r := 0; r < numReplicas; r++ {
    replicaNode = (shardID + r + 1) % nodeCount
}
```

**设计意图：**
- `+0` 确保 Primary 从固定节点开始
- `+r+1` 确保 Replica 和 Primary 不在同一节点
- 错开设计保证故障时 Primary 和 Replica 不会同时失效

---

## 六、测试覆盖

| 测试文件 | 测试内容 |
|---------|---------|
| `cluster_test.go` | 节点加入、状态版本、Master 拒绝非主节点加入 |
| `shard_test.go` | 分片路由、自动启动、文档索引、跨节点路由错误 |
| `distributed_test.go` | 分布式索引、副本同步 |
| `search_test.go` | 分布式搜索、Replica 故障降级 |
| `server_test.go` | HTTP API：创建索引、索引+搜索、布尔查询、短语查询、健康检查、集群状态、错误处理 |

**总计：20+ 测试用例，全部通过。**

---

## 七、如何运行

### 单节点模式
```bash
cd es/cmd/es
go run main.go -node=node-0 -http=:9200
```

### 三节点集群
```bash
# Terminal 1（Master）
go run main.go -node=node-0 -http=:9200

# Terminal 2
go run main.go -node=node-1 -http=:9201 -join=http://localhost:9200

# Terminal 3
go run main.go -node=node-2 -http=:9202 -join=http://localhost:9200
```

### 测试 API
```bash
# 创建索引（3 分片 1 副本）
curl -X PUT "http://localhost:9200/products"

# 索引文档
curl -X POST "http://localhost:9200/products/_doc/1" \
  -H "Content-Type: application/json" \
  -d '{"title":"iPhone 15","price":5999}'

# 搜索
curl "http://localhost:9200/products/_search?q=iPhone"

# 集群健康
curl "http://localhost:9200/_cluster/health"

# 集群状态
curl "http://localhost:9200/_cluster/state"
```

---

## 八、架构图

```
┌─────────────────────────────────────────┐
│           HTTP REST API Layer           │
│  PUT /:index  POST /_doc  GET /_search  │
└─────────────────────────────────────────┘
                   │
┌─────────────────────────────────────────┐
│         Node (Coordinator)              │
│  ┌─────────┐  ┌─────────┐            │
│  │Cluster  │  │Transport│            │
│  │State    │  │(HTTP)   │            │
│  └─────────┘  └─────────┘            │
└─────────────────────────────────────────┘
                   │
      ┌────────────┼────────────┐
      │            │            │
┌─────▼─────┐ ┌────▼─────┐ ┌───▼──────┐
│ Shard 0   │ │ Shard 1  │ │ Shard 2  │
│ Primary   │ │ Replica  │ │ Primary  │
│ (lucy)    │ │ (lucy)   │ │ (lucy)   │
└───────────┘ └──────────┘ └──────────┘
      │            │            │
      └────────────┼────────────┘
                   │
         ┌─────────▼─────────┐
         │   Disk (Index)    │
         └───────────────────┘
```

---

## 九、与真实 ES 的差异

| 特性 | TinyES | 真实 Elasticsearch |
|------|--------|-------------------|
| 分片副本 | ✅ 固定配置 | ✅ 动态调整 |
| Master 选举 | ❌ 固定 Master | ✅ Zen Discovery |
| 故障检测 | ❌ 无 | ✅ Fault Detection |
| 数据迁移 | ❌ 无 | ✅ Shard Reallocation |
| 查询 DSL | ❌ 简单字符串 | ✅ JSON DSL |
| 聚合查询 | ❌ 无 | ✅ Aggregations |
| 持久化 | ✅ 磁盘索引 | ✅ Translog + Segment |
| 分布式事务 | ❌ 无 | ✅ 乐观并发控制 |

---

## 十、学习收获

1. **分片与副本**：理解了数据分布和容错的基本机制
2. **分布式查询**：并行查询 + 结果聚合的模式
3. **集群状态管理**：全局状态视图是协调分布式操作的关键
4. **轮询分配**：简单的负载均衡策略
5. **Go 并发**：大量使用 goroutine + channel 实现并行搜索

---

> 项目代码：`es-complete.zip`
> 测试命令：`go test ./... -v`
> 作者：TinyES Team
