package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"open-ima/internal/application/port"
)

// Message 是 port.ChatMessage 的别名,ChatClient 即 port.ChatModel 的实现。
type Message = port.ChatMessage

var _ port.ChatModel = (*ChatClient)(nil)

type ChatClient struct {
	protocol string
	baseURL  string
	apiKey   string
	model    string
	hc       *http.Client
}

func NewChatClient(baseURL, apiKey, model string) *ChatClient {
	return NewChatClientWithProtocol("openai", baseURL, apiKey, model)
}

func NewChatClientWithProtocol(protocol, baseURL, apiKey, model string) *ChatClient {
	if protocol == "" {
		protocol = "openai"
	}
	return &ChatClient{
		protocol: strings.ToLower(protocol), baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model,
		hc: &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *ChatClient) Complete(ctx context.Context, messages []Message) (string, error) {
	resp, err := c.request(ctx, messages, nil, false)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if c.protocol == "anthropic" {
		var result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return "", fmt.Errorf("chat: decode: %w", err)
		}
		var output strings.Builder
		for _, block := range result.Content {
			if block.Type == "text" {
				output.WriteString(block.Text)
			}
		}
		if output.Len() == 0 {
			return "", fmt.Errorf("chat: response has no text content")
		}
		return output.String(), nil
	}
	var result struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("chat: decode: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("chat: response has no choices")
	}
	return result.Choices[0].Message.Content, nil
}

func (c *ChatClient) Stream(ctx context.Context, messages []Message, onToken func(string) error) error {
	resp, err := c.request(ctx, messages, nil, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil
		}
		if c.protocol == "anthropic" {
			var event struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				return fmt.Errorf("chat stream: decode: %w", err)
			}
			if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				if err := onToken(event.Delta.Text); err != nil {
					return err
				}
			}
			continue
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("chat stream: decode: %w", err)
		}
		for _, choice := range event.Choices {
			if choice.Delta.Content != "" {
				if err := onToken(choice.Delta.Content); err != nil {
					return err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("chat stream: read: %w", err)
	}
	return nil
}

func (c *ChatClient) request(ctx context.Context, messages []Message, tools []port.ToolDef, stream bool) (*http.Response, error) {
	if c.protocol != "openai" && c.protocol != "anthropic" {
		return nil, fmt.Errorf("chat: unsupported protocol %q", c.protocol)
	}
	var endpoint string
	var payload map[string]any
	if c.protocol == "anthropic" {
		endpoint = "/v1/messages"
		if strings.HasSuffix(c.baseURL, "/v1") {
			endpoint = "/messages"
		}
		system, conversation := anthropicMessages(messages)
		payload = map[string]any{"model": c.model, "messages": conversation, "system": system, "max_tokens": 4096, "stream": stream}
		if len(tools) > 0 {
			payload["tools"] = anthropicTools(tools)
		}
	} else {
		endpoint = "/chat/completions"
		payload = map[string]any{"model": c.model, "messages": openaiMessages(messages), "stream": stream}
		if len(tools) > 0 {
			payload["tools"] = openaiTools(tools)
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.protocol == "anthropic" {
		req.Header.Set("x-api-key", c.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		body := strings.TrimSpace(string(data))
		if len(tools) > 0 && looksLikeToolRejection(resp.StatusCode, body) {
			return nil, fmt.Errorf("%w: status %d: %s", port.ErrToolsUnsupported, resp.StatusCode, body)
		}
		return nil, fmt.Errorf("chat: status %d: %s", resp.StatusCode, body)
	}
	return resp, nil
}

// looksLikeToolRejection 识别模型端拒绝 tools 参数的 400 响应。
func looksLikeToolRejection(status int, body string) bool {
	if status != http.StatusBadRequest {
		return false
	}
	lower := strings.ToLower(body)
	return strings.Contains(lower, "tool")
}
