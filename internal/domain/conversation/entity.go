// Package conversation 承载会话聚合:会话、消息与引用实体,仓储契约与领域服务。
package conversation

import "time"

// Citation 是回答引用到的分块证据。
type Citation struct {
	DocumentID string  `json:"document_id"`
	Title      string  `json:"title"`
	ChunkID    string  `json:"chunk_id"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score"`
}

// Conversation 是属于某个知识库的一轮对话。
type Conversation struct {
	ID        string    `json:"id"`
	KBID      string    `json:"kb_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

// Message 是会话中的一条消息;助手消息携带引用。
type Message struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation_id"`
	Role           string     `json:"role"`
	Content        string     `json:"content"`
	Citations      []Citation `json:"citations"`
	CreatedAt      time.Time  `json:"created_at"`
}
