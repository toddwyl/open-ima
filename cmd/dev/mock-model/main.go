package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
)

func main() {
	addr := flag.String("addr", ":8200", "listen address")
	flag.Parse()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/chat/completions", handleChat)
	log.Printf("mock model listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Stream   bool             `json:"stream"`
		Tools    []map[string]any `json:"tools"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(request.Tools) > 0 {
		handleToolsChat(w, request.Messages)
		return
	}
	if !request.Stream {
		writeJSON(w, map[string]any{"choices": []any{map[string]any{
			"message": map[string]string{"role": "assistant", "content": "open ima smoke document"},
		}}})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	answer := "Open IMA 端到端链路正常。[1]"
	for _, message := range request.Messages {
		if strings.Contains(message.Content, "ORCHID-7429") {
			answer = "The launch code in the PDF is ORCHID-7429. [1]"
			break
		}
	}
	for _, token := range strings.SplitAfter(answer, " ") {
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", token)
		if flusher != nil {
			flusher.Flush()
		}
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

// handleToolsChat 模拟支持 tools 的模型:第一轮要求检索知识库,拿到工具结果后给出引用答案。
func handleToolsChat(w http.ResponseWriter, messages []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}) {
	hasToolResult := false
	grounded := false
	lastUserQuery := ""
	for _, message := range messages {
		switch message.Role {
		case "tool":
			hasToolResult = true
			if strings.Contains(message.Content, "ORCHID-7429") {
				grounded = true
			}
		case "user":
			lastUserQuery = message.Content
		}
	}
	if !hasToolResult {
		arguments, _ := json.Marshal(map[string]string{"query": lastUserQuery})
		writeJSON(w, map[string]any{"choices": []any{map[string]any{
			"finish_reason": "tool_calls",
			"message": map[string]any{
				"role":    "assistant",
				"content": "先在知识库中检索相关内容。",
				"tool_calls": []any{map[string]any{
					"id":   "call_1",
					"type": "function",
					"function": map[string]string{
						"name":      "search_knowledge",
						"arguments": string(arguments),
					},
				}},
			},
		}}})
		return
	}
	answer := "Open IMA 端到端链路正常。[c1]"
	if grounded {
		answer = "The launch code in the PDF is ORCHID-7429. [c1]"
	}
	writeJSON(w, map[string]any{"choices": []any{map[string]any{
		"finish_reason": "stop",
		"message":       map[string]string{"role": "assistant", "content": answer},
	}}})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
