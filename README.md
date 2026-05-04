# TinyElasticSearch (ES) 实现说明

## 项目概述

一个从零实现的极简分布式搜索引擎，兼容 Elasticsearch 核心概念。单 Go 模块，无外部依赖。

## 已实现特性

| 特性 | 说明 |
|------|------|
| **集群管理** | 多节点集群，Master 选举，节点加入/离开 |
| **分片机制** | 索引分片 + 副本分配，轮询负载均衡 |
| **分布式索引** | 文档路由到对应分片，自动 replica 同步 |
| **分布式搜索** | 跨 shard 并行查询，Top-K 截断合并 |
| **副本容错** | Primary 失效时自动从 Replica 读取 |
| **HTTP API** | RESTful 接口，兼容 ES 风格 |
| **WAL 持久化** | 操作日志，崩溃恢复基础 |

## 项目结构

```
es/
├── cluster/              # 集群核心
│   ├── node.go           # Node 主逻辑（Master/Worker 角色）
│   ├── shard.go          # ShardEngine（单分片索引/查询）
│   ├── shard_manager.go  # 分片分配策略
│   ├── cluster_state.go  # 集群状态机
│   ├── transport.go      # 节点间通信
│   └── errors.go         # 错误定义
├── server/               # HTTP 服务层
│   ├── server.go         # REST API 实现
│   └── server_test.go    # API 测试
└── cmd/es/               # 启动入口
    └── main.go           # 多节点集群启动
```

## 核心流程

### 1. 创建索引
```
PUT /:index
→ Node.CreateIndex() → 计算 shard 分配 → 各节点启动 ShardEngine
```

### 2. 索引文档
```
POST /:index/_doc/:id
→ Node.Index() → 路由到目标 shard → ShardEngine.Index()
→ 异步同步到 Replica
```

### 3. 搜索
```
GET /:index/_search?q=query
→ Node.Search() → 并行查询所有相关 shard → 合并 Top-K 结果
```

## 启动多节点集群

```bash
# 启动 3 节点集群
go run cmd/es/main.go -node=node-0 -http=:9200 &
go run cmd/es/main.go -node=node-1 -http=:9201 -join=localhost:9200 &
go run cmd/es/main.go -node=node-2 -http=:9202 -join=localhost:9200 &
```

## 测试

```bash
cd es
go test ./... -v
```

共 18 个测试全部通过：
- cluster 包：10 个测试（集群、分片、分布式搜索）
- server 包：8 个测试（HTTP API）

## 技术栈

- Go 1.21+
- 无外部依赖（标准库 only）
- 内存索引（基于 Lucy 倒排引擎）
- 磁盘 WAL 持久化

## 与 Lucy 的关系

ES 复用 Lucy 的底层索引引擎（ShardEngine 封装），在其之上构建分布式层（集群、分片、副本、协调）。
