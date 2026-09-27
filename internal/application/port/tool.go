package port

import (
	"context"
	"encoding/json"
)

// ToolResult 是一次工具执行的结果;Output 给模型读,Data 供事件与持久化使用。
type ToolResult struct {
	Success bool
	Output  string
	Data    map[string]any
	Error   string
}

// Tool 是 agent 可调用的工具;Parameters 为 JSON Schema。
type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error)
}
