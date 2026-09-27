// Package conversation 承载会话聚合:会话、消息与引用实体,仓储契约与领域服务。
package conversation

import "time"

// Citation 是回答引用到的分块证据。
type Citation struct {
	MediaBizID string  `json:"media_biz_id"`
	Title      string  `json:"title"`
	ChunkBizID string  `json:"chunk_biz_id"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score"`
}

// Conversation 是属于某个知识库的一轮对话。
type Conversation struct {
	ID        int64     `json:"id"`
	BizID     string    `json:"biz_id"`
	KBBizID   string    `json:"kb_biz_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

// Message 是会话中的一条消息;助手消息携带引用。
type Message struct {
	ID                int64      `json:"id"`
	BizID             string     `json:"biz_id"`
	ConversationBizID string     `json:"conversation_biz_id"`
	Role              string     `json:"role"`
	Content           string     `json:"content"`
	Citations         []Citation `json:"citations"`
	CreatedAt         time.Time  `json:"created_at"`
}
