package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"open-ima/internal/application/port"
)

var testToolDefs = []port.ToolDef{{
	Name:        "search_knowledge",
	Description: "Search the knowledge base.",
	Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
}}

func TestOpenAICompleteTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model    string           `json:"model"`
			Messages []map[string]any `json:"messages"`
			Tools    []map[string]any `json:"tools"`
			Stream   bool             `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(request.Tools) != 1 || request.Tools[0]["type"] != "function" {
			t.Errorf("tools = %+v", request.Tools)
		}
		if request.Stream {
			t.Errorf("CompleteTools must not stream")
		}
		if len(request.Messages) != 3 {
			t.Fatalf("messages = %+v", request.Messages)
		}
		assistant := request.Messages[1]
		calls, _ := assistant["tool_calls"].([]any)
		if assistant["role"] != "assistant" || len(calls) != 1 {
			t.Errorf("assistant tool_calls = %+v", assistant)
		}
		call, _ := calls[0].(map[string]any)
		fn, _ := call["function"].(map[string]any)
		if call["type"] != "function" || fn["name"] != "search_knowledge" {
			t.Errorf("tool call = %+v", call)
		}
		if _, ok := fn["arguments"].(string); !ok {
			t.Errorf("arguments must be a JSON string, got %T", fn["arguments"])
		}
		toolMessage := request.Messages[2]
		if toolMessage["role"] != "tool" || toolMessage["tool_call_id"] != "call-1" {
			t.Errorf("tool message = %+v", toolMessage)
		}
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":"让我查一下。","reasoning_content":"plan","tool_calls":[{"id":"call-2","type":"function","function":{"name":"search_knowledge","arguments":"{\"query\":\"二期\"}"}}]}}]}`)
	}))
	defer server.Close()
	client := NewChatClient(server.URL, "", "m")
	response, err := client.CompleteTools(context.Background(), []Message{
		{Role: "user", Content: "二期计划?"},
		{Role: "assistant", Content: "先看一期。", ToolCalls: []port.LLMToolCall{{ID: "call-1", Name: "search_knowledge", Arguments: json.RawMessage(`{"query":"一期"}`)}}},
		{Role: "tool", ToolCallID: "call-1", Name: "search_knowledge", Content: "一期结果"},
	}, testToolDefs)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if response.Content != "让我查一下。" || response.Reasoning != "plan" || response.FinishReason != "tool_calls" {
		t.Errorf("response = %+v", response)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].ID != "call-2" || response.ToolCalls[0].Name != "search_knowledge" {
		t.Fatalf("tool calls = %+v", response.ToolCalls)
	}
	if string(response.ToolCalls[0].Arguments) != `{"query":"二期"}` {
		t.Errorf("arguments = %s", response.ToolCalls[0].Arguments)
	}
}

func TestAnthropicCompleteTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			System   string           `json:"system"`
			Messages []map[string]any `json:"messages"`
			Tools    []map[string]any `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.System != "rules" {
			t.Errorf("system = %q", request.System)
		}
		if len(request.Tools) != 1 {
			t.Fatalf("tools = %+v", request.Tools)
		}
		if _, ok := request.Tools[0]["input_schema"]; !ok {
			t.Errorf("anthropic tool must use input_schema: %+v", request.Tools[0])
		}
		// 期望(system 被抽取):assistant(text+tool_use) → user(tool_result) → user
		if len(request.Messages) != 3 {
			t.Fatalf("messages = %+v", request.Messages)
		}
		assistant := request.Messages[0]
		blocks, _ := assistant["content"].([]any)
		if assistant["role"] != "assistant" || len(blocks) != 2 {
			t.Errorf("assistant blocks = %+v", assistant)
		}
		toolUse, _ := blocks[1].(map[string]any)
		if toolUse["type"] != "tool_use" || toolUse["name"] != "search_knowledge" {
			t.Errorf("tool_use = %+v", toolUse)
		}
		if _, ok := toolUse["input"].(map[string]any); !ok {
			t.Errorf("tool_use input must be an object, got %T", toolUse["input"])
		}
		toolResult := request.Messages[1]
		results, _ := toolResult["content"].([]any)
		first, _ := results[0].(map[string]any)
		if toolResult["role"] != "user" || first["type"] != "tool_result" || first["tool_use_id"] != "call-1" {
			t.Errorf("tool_result message = %+v", toolResult)
		}
		_, _ = io.WriteString(w, `{"stop_reason":"tool_use","content":[{"type":"text","text":"继续查"},{"type":"tool_use","id":"call-9","name":"web_search","input":{"query":"x"}}]}`)
	}))
	defer server.Close()
	client := NewChatClientWithProtocol("anthropic", server.URL, "key", "m")
	response, err := client.CompleteTools(context.Background(), []Message{
		{Role: "system", Content: "rules"},
		{Role: "assistant", Content: "先搜库。", ToolCalls: []port.LLMToolCall{{ID: "call-1", Name: "search_knowledge", Arguments: json.RawMessage(`{"query":"q"}`)}}},
		{Role: "tool", ToolCallID: "call-1", Content: "结果"},
		{Role: "user", Content: "最新进展?"},
	}, testToolDefs)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if response.Content != "继续查" || response.FinishReason != "stop" {
		t.Errorf("response = %+v", response)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].ID != "call-9" || string(response.ToolCalls[0].Arguments) != `{"query":"x"}` {
		t.Errorf("tool calls = %+v", response.ToolCalls)
	}
}

func TestAnthropicConsecutiveToolResultsMerge(t *testing.T) {
	system, messages := anthropicMessages([]Message{
		{Role: "system", Content: "a"},
		{Role: "system", Content: "b"},
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []port.LLMToolCall{{ID: "c1", Name: "t"}, {ID: "c2", Name: "t"}}},
		{Role: "tool", ToolCallID: "c1", Content: "r1"},
		{Role: "tool", ToolCallID: "c2", Content: "r2"},
	})
	if system != "a\n\nb" {
		t.Errorf("system = %q", system)
	}
	if len(messages) != 3 {
		t.Fatalf("messages = %+v", messages)
	}
	results, _ := messages[2]["content"].([]map[string]any)
	if messages[2]["role"] != "user" || len(results) != 2 {
		t.Errorf("merged tool results = %+v", messages[2])
	}
}

func TestCompleteToolsUnsupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"tools is not supported by this model"}}`, http.StatusBadRequest)
	}))
	defer server.Close()
	_, err := NewChatClient(server.URL, "", "m").CompleteTools(context.Background(),
		[]Message{{Role: "user", Content: "q"}}, testToolDefs)
	if !errors.Is(err, port.ErrToolsUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestCompleteToolsMalformedArguments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"c","type":"function","function":{"name":"t","arguments":"{oops"}}]}}]}`)
	}))
	defer server.Close()
	_, err := NewChatClient(server.URL, "", "m").CompleteTools(context.Background(),
		[]Message{{Role: "user", Content: "q"}}, testToolDefs)
	if !errors.Is(err, port.ErrToolsUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestCompleteToolsRequiresTools(t *testing.T) {
	_, err := NewChatClient("http://unused", "", "m").CompleteTools(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "at least one tool") {
		t.Fatalf("err = %v", err)
	}
}
