package port

import (
	"context"
	"encoding/json"
	"errors"
)

// ChatMessage 是一条聊天消息;ToolCalls/ToolCallID 仅在工具调用往返中使用。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// ToolCalls 是助手消息发起的工具调用(function calling)。
	ToolCalls []LLMToolCall `json:"tool_calls,omitempty"`
	// ToolCallID 与 Name 仅用于 role=tool 的结果消息,关联对应调用。
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
}

// ToolDef 是提供给模型的工具定义,Parameters 为 JSON Schema。
type ToolDef struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// LLMToolCall 是模型返回的一次工具调用;Arguments 原文保留,便于回放与审计。
type LLMToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// ChatResponse 是一次带工具调用的模型响应。
type ChatResponse struct {
	Content      string
	Reasoning    string // reasoning_content 透传
	ToolCalls    []LLMToolCall
	FinishReason string // stop / length / content_filter ...
}

// ErrToolsUnsupported 表示模型不支持(或拒绝)工具调用;调用方应降级处理。
var ErrToolsUnsupported = errors.New("chat model does not support tool calling")

// ChatModel 是聊天模型能力端口。
type ChatModel interface {
	Complete(ctx context.Context, messages []ChatMessage) (string, error)
	Stream(ctx context.Context, messages []ChatMessage, onToken func(string) error) error
	// CompleteTools 携带工具定义做一轮非流式补全;模型不支持 tools 时返回 ErrToolsUnsupported。
	CompleteTools(ctx context.Context, messages []ChatMessage, tools []ToolDef) (*ChatResponse, error)
}
