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
)

func TestChatComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		var request struct {
			Model    string    `json:"model"`
			Messages []Message `json:"messages"`
			Stream   bool      `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Model != "chat-model" || request.Stream || len(request.Messages) != 1 {
			t.Errorf("request = %+v", request)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"rewritten query"}}]}`)
	}))
	defer server.Close()
	client := NewChatClient(server.URL, "key", "chat-model")
	result, err := client.Complete(context.Background(), []Message{{Role: "user", Content: "q"}})
	if err != nil || result != "rewritten query" {
		t.Fatalf("result=%q err=%v", result, err)
	}
}

func TestChatStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n")
		_, _ = io.WriteString(w, "event: ignored\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client := NewChatClient(server.URL, "", "m")
	var output strings.Builder
	err := client.Stream(context.Background(), []Message{{Role: "user", Content: "q"}}, func(token string) error {
		output.WriteString(token)
		return nil
	})
	if err != nil || output.String() != "你好" {
		t.Fatalf("output=%q err=%v", output.String(), err)
	}
}

func TestChatStreamPropagatesCallbackError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n")
	}))
	defer server.Close()
	want := errors.New("write failed")
	err := NewChatClient(server.URL, "", "m").Stream(context.Background(), nil, func(string) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func TestChatHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, err := NewChatClient(server.URL, "", "m").Complete(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnthropicComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "key" || r.Header.Get("anthropic-version") == "" {
			t.Errorf("request path=%s headers=%v", r.URL.Path, r.Header)
		}
		var request struct {
			Model     string    `json:"model"`
			System    string    `json:"system"`
			Messages  []Message `json:"messages"`
			MaxTokens int       `json:"max_tokens"`
			Stream    bool      `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Model != "kimi-for-coding" || request.System != "rules" || len(request.Messages) != 1 || request.MaxTokens != 4096 || request.Stream {
			t.Errorf("request = %+v", request)
		}
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"answer"}]}`)
	}))
	defer server.Close()
	client := NewChatClientWithProtocol("anthropic", server.URL, "key", "kimi-for-coding")
	result, err := client.Complete(context.Background(), []Message{{Role: "system", Content: "rules"}, {Role: "user", Content: "q"}})
	if err != nil || result != "answer" {
		t.Fatalf("result=%q err=%v", result, err)
	}
}

func TestAnthropicStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"你\"}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"好\"}}\n\n")
		_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()
	client := NewChatClientWithProtocol("anthropic", server.URL+"/v1", "key", "m")
	var output strings.Builder
	err := client.Stream(context.Background(), []Message{{Role: "user", Content: "q"}}, func(token string) error {
		output.WriteString(token)
		return nil
	})
	if err != nil || output.String() != "你好" {
		t.Fatalf("output=%q err=%v", output.String(), err)
	}
}

func TestUnsupportedChatProtocol(t *testing.T) {
	_, err := NewChatClientWithProtocol("invalid", "http://unused", "", "m").Complete(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported protocol") {
		t.Fatalf("err = %v", err)
	}
}
