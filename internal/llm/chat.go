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
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatClient struct {
	baseURL string
	apiKey  string
	model   string
	hc      *http.Client
}

func NewChatClient(baseURL, apiKey, model string) *ChatClient {
	return &ChatClient{
		baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model,
		hc: &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *ChatClient) Complete(ctx context.Context, messages []Message) (string, error) {
	resp, err := c.request(ctx, messages, false)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
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
	resp, err := c.request(ctx, messages, true)
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

func (c *ChatClient) request(ctx context.Context, messages []Message, stream bool) (*http.Response, error) {
	body, err := json.Marshal(map[string]any{
		"model": c.model, "messages": messages, "stream": stream,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("chat: status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return resp, nil
}
