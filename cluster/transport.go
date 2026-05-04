package cluster

// MessageType 节点间消息类型
type MessageType int

const (
	JoinRequest  MessageType = iota // 新节点请求加入
	JoinResponse                     // Master 回复加入确认
	StateUpdate                      // Master 广播集群状态变更
	Heartbeat                        // 心跳（简化版暂不用）
	IndexRequest                     // 写入请求（协调节点 -> 主分片节点）
	IndexResponse                    // 写入响应（主分片节点 -> 协调节点）
	ReplicaSync                      // 副本同步（主分片 -> 副本节点）
	SearchRequest                    // 查询请求（协调节点 -> 分片节点）
	SearchResponse                   // 查询响应（分片节点 -> 协调节点）
	DeleteIndexRequest               // 删除索引请求（Master -> 各节点）
	DeleteIndexResponse              // 删除索引响应（各节点 -> Master）
	ShardMigrateRequest              // 分片迁移请求（Master -> 源/目标节点）
	ShardMigrateResponse             // 分片迁移响应
)

// Message 节点间传输的消息
type Message struct {
	Type      MessageType
	From      string // 发送者节点 ID
	RequestID string // 可选，用于请求-响应对应
	Payload   []byte // JSON 序列化的负载
}

// Transport 模拟网络传输层
type Transport interface {
	// Send 发送消息到目标节点
	Send(to string, msg Message) error
	// Inbox 返回本节点的消息接收通道
	Inbox() <-chan Message
	// Register 注册节点地址（模拟网络拓扑发现）
	Register(nodeID string)
	// Address 返回本节点地址标识
	Address() string
}

// InMemoryTransport 用 Go channel 模拟网络
type InMemoryTransport struct {
	nodeID   string
	hub      *TransportHub
	inbox    chan Message
	address  string
}

// TransportHub 管理所有节点的通信通道（模拟网络交换机）
type TransportHub struct {
	transports map[string]*InMemoryTransport
}

// NewTransportHub 创建通信中心
func NewTransportHub() *TransportHub {
	return &TransportHub{
		transports: make(map[string]*InMemoryTransport),
	}
}

// CreateTransport 为指定节点创建传输层
func (h *TransportHub) CreateTransport(nodeID string) *InMemoryTransport {
	t := &InMemoryTransport{
		nodeID:  nodeID,
		hub:     h,
		inbox:   make(chan Message, 100),
		address: nodeID, // 简化为节点 ID 即地址
	}
	h.transports[nodeID] = t
	return t
}

// RemoveTransport 移除节点（模拟节点离线）
func (h *TransportHub) RemoveTransport(nodeID string) {
	delete(h.transports, nodeID)
}

// Send 发送消息到目标节点
func (t *InMemoryTransport) Send(to string, msg Message) error {
	target, ok := t.hub.transports[to]
	if !ok {
		return ErrNodeNotFound
	}
	// 异步投递，模拟网络延迟为 0（测试可控）
	go func() { target.inbox <- msg }()
	return nil
}

// Inbox 返回本节点的消息接收通道
func (t *InMemoryTransport) Inbox() <-chan Message {
	return t.inbox
}

// Register 注册节点地址（模拟服务发现）
func (t *InMemoryTransport) Register(nodeID string) {
	// 在 InMemoryTransport 中，Register 是空操作
	// 所有节点已经在 hub 中注册
}

// Address 返回本节点地址标识
func (t *InMemoryTransport) Address() string {
	return t.address
}
