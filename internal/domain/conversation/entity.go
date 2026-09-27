// Package conversation 承载会话聚合:会话、消息与引用实体,仓储契约与领域服务。
package conversation

import (
	"encoding/json"
	"time"
)

// 引用来源类型;缺省(空串)兼容旧数据,视为 kb_chunk。
const (
	SourceTypeKBChunk = "kb_chunk"
	SourceTypeWeb     = "web"
)

// 会话问答模式;缺省(空串)兼容旧数据,视为 agent。
const (
	ModeQuick = "quick"
	ModeAgent = "agent"
)

// Citation 是回答引用到的证据;kb_chunk 指向库内分块,web 指向网页 URL。
type Citation struct {
	SourceType string  `json:"source_type,omitempty"`
	MediaBizID string  `json:"media_biz_id,omitempty"`
	Title      string  `json:"title"`
	ChunkBizID string  `json:"chunk_biz_id,omitempty"`
	URL        string  `json:"url,omitempty"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score,omitempty"`
}

// AgentToolCall 是步骤轨迹中的一次工具调用记录。
type AgentToolCall struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Args       json.RawMessage `json:"args,omitempty"`
	Success    bool            `json:"success"`
	Output     string          `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
	DurationMs int64           `json:"duration_ms,omitempty"`
}

// AgentStep 是 ReAct 一轮的步骤轨迹;Handles 记录本轮新分配的句柄映射(句柄 → 业务键)。
type AgentStep struct {
	Iteration int               `json:"iteration"`
	Thought   string            `json:"thought,omitempty"`
	Reasoning string            `json:"reasoning,omitempty"`
	ToolCalls []AgentToolCall   `json:"tool_calls,omitempty"`
	Handles   map[string]string `json:"handles,omitempty"`
	Truncated bool              `json:"truncated,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// Conversation 是属于某个知识库的一轮对话。
type Conversation struct {
	ID        int64     `json:"id"`
	BizID     string    `json:"biz_id"`
	KBBizID   string    `json:"kb_biz_id"`
	Title     string    `json:"title"`
	Mode      string    `json:"mode"`
	CreatedAt time.Time `json:"created_at"`
}

// Message 是会话中的一条消息;助手消息携带引用与 agent 步骤轨迹(旧数据为 nil)。
type Message struct {
	ID                int64       `json:"id"`
	BizID             string      `json:"biz_id"`
	ConversationBizID string      `json:"conversation_biz_id"`
	Role              string      `json:"role"`
	Content           string      `json:"content"`
	Citations         []Citation  `json:"citations"`
	AgentSteps        []AgentStep `json:"agent_steps,omitempty"`
	CreatedAt         time.Time   `json:"created_at"`
}
