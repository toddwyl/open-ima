package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"open-ima/internal/application/port"
)

// CompleteTools 携带工具定义做一轮非流式补全,统一映射为 port.ChatResponse。
func (c *ChatClient) CompleteTools(ctx context.Context, messages []Message, tools []port.ToolDef) (*port.ChatResponse, error) {
	if len(tools) == 0 {
		return nil, fmt.Errorf("chat: CompleteTools requires at least one tool")
	}
	resp, err := c.request(ctx, messages, tools, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if c.protocol == "anthropic" {
		return decodeAnthropicToolsResponse(resp.Body)
	}
	return decodeOpenAIToolsResponse(resp.Body)
}

// ---- OpenAI 协议编码 ----

// openaiMessages 把内部消息编码为 OpenAI wire 格式;
// 工具调用的 arguments 以字符串承载(OpenAI 协议要求)。
func openaiMessages(messages []Message) []map[string]any {
	encoded := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		item := map[string]any{"role": message.Role, "content": message.Content}
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				calls = append(calls, map[string]any{
					"id":   call.ID,
					"type": "function",
					"function": map[string]any{
						"name":      call.Name,
						"arguments": string(call.Arguments),
					},
				})
			}
			item["tool_calls"] = calls
		}
		if message.Role == "tool" {
			item["tool_call_id"] = message.ToolCallID
			if message.Name != "" {
				item["name"] = message.Name
			}
		}
		encoded = append(encoded, item)
	}
	return encoded
}

func openaiTools(tools []port.ToolDef) []map[string]any {
	encoded := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		encoded = append(encoded, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.Parameters,
			},
		})
	}
	return encoded
}

func decodeOpenAIToolsResponse(resp io.Reader) (*port.ChatResponse, error) {
	var result struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning_content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp).Decode(&result); err != nil {
		return nil, fmt.Errorf("chat tools: decode: %w", err)
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("%w: response has no choices", port.ErrToolsUnsupported)
	}
	choice := result.Choices[0]
	response := &port.ChatResponse{
		Content:      choice.Message.Content,
		Reasoning:    choice.Message.Reasoning,
		FinishReason: choice.FinishReason,
	}
	for _, call := range choice.Message.ToolCalls {
		arguments := json.RawMessage(call.Function.Arguments)
		if len(arguments) == 0 {
			arguments = json.RawMessage("{}")
		}
		if !json.Valid(arguments) {
			return nil, fmt.Errorf("%w: tool call %q has malformed arguments", port.ErrToolsUnsupported, call.Function.Name)
		}
		response.ToolCalls = append(response.ToolCalls, port.LLMToolCall{
			ID: call.ID, Name: call.Function.Name, Arguments: arguments,
		})
	}
	return response, nil
}

// ---- Anthropic 协议编码 ----

// anthropicMessages 把内部消息编码为 Anthropic wire 格式:
// system 抽取为顶层字段;助手 tool_calls 映射为 tool_use 块;
// 连续的 tool 结果消息合并为一条 user 消息的 tool_result 块序列。
func anthropicMessages(messages []Message) (string, []map[string]any) {
	var system strings.Builder
	conversation := make([]map[string]any, 0, len(messages))
	var pendingToolResults []map[string]any
	flush := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		conversation = append(conversation, map[string]any{"role": "user", "content": pendingToolResults})
		pendingToolResults = nil
	}
	for _, message := range messages {
		switch {
		case message.Role == "system":
			if system.Len() > 0 {
				system.WriteString("\n\n")
			}
			system.WriteString(message.Content)
		case message.Role == "tool":
			pendingToolResults = append(pendingToolResults, map[string]any{
				"type": "tool_result", "tool_use_id": message.ToolCallID, "content": message.Content,
			})
		case message.Role == "assistant" && len(message.ToolCalls) > 0:
			flush()
			content := make([]map[string]any, 0, len(message.ToolCalls)+1)
			if message.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": message.Content})
			}
			for _, call := range message.ToolCalls {
				input := json.RawMessage(call.Arguments)
				if !json.Valid(input) {
					input = json.RawMessage("{}")
				}
				content = append(content, map[string]any{
					"type": "tool_use", "id": call.ID, "name": call.Name, "input": input,
				})
			}
			conversation = append(conversation, map[string]any{"role": "assistant", "content": content})
		default:
			flush()
			conversation = append(conversation, map[string]any{"role": message.Role, "content": message.Content})
		}
	}
	flush()
	return system.String(), conversation
}

func anthropicTools(tools []port.ToolDef) []map[string]any {
	encoded := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		encoded = append(encoded, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": tool.Parameters,
		})
	}
	return encoded
}

func decodeAnthropicToolsResponse(resp io.Reader) (*port.ChatResponse, error) {
	var result struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.NewDecoder(resp).Decode(&result); err != nil {
		return nil, fmt.Errorf("chat tools: decode: %w", err)
	}
	if len(result.Content) == 0 {
		return nil, fmt.Errorf("%w: response has no content blocks", port.ErrToolsUnsupported)
	}
	response := &port.ChatResponse{FinishReason: anthropicFinishReason(result.StopReason)}
	var content strings.Builder
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			content.WriteString(block.Text)
		case "tool_use":
			input := block.Input
			if len(input) == 0 {
				input = json.RawMessage("{}")
			}
			response.ToolCalls = append(response.ToolCalls, port.LLMToolCall{ID: block.ID, Name: block.Name, Arguments: input})
		}
	}
	response.Content = content.String()
	return response, nil
}

// anthropicFinishReason 把 Anthropic stop_reason 归一到 OpenAI 风格的 finish 词汇。
func anthropicFinishReason(stopReason string) string {
	switch stopReason {
	case "end_turn", "tool_use":
		return "stop"
	case "max_tokens":
		return "length"
	default:
		return stopReason
	}
}
