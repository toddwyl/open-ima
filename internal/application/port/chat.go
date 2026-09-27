package port

import "context"

// ChatMessage 是一条聊天消息。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatModel 是聊天模型能力端口。
type ChatModel interface {
	Complete(ctx context.Context, messages []ChatMessage) (string, error)
	Stream(ctx context.Context, messages []ChatMessage, onToken func(string) error) error
}
