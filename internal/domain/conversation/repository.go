package conversation

import "context"

// Repository 是会话聚合的持久化契约,仅定义接口,实现位于 infrastructure。
type ConversationRepository interface {
	// Get 按 ID 与所属知识库双重匹配,防止跨库访问;未命中返回 ErrNotFound。
	Get(ctx context.Context, id, kbID string) (*Conversation, error)
	Insert(ctx context.Context, conversation *Conversation) error
	ListByKB(ctx context.Context, kbID string) ([]Conversation, error)
	// DeleteByKB 删除知识库下全部会话及其消息。
	DeleteByKB(ctx context.Context, kbID string) error
	// RecentMessages 按时间正序返回最近 limit 条消息。
	RecentMessages(ctx context.Context, conversationID string, limit int) ([]Message, error)
	ListMessages(ctx context.Context, conversationID string) ([]Message, error)
	AppendMessage(ctx context.Context, message *Message) error
}
