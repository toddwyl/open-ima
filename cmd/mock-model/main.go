package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/chat/completions", handleChat)
	log.Printf("mock model listening on :8200")
	log.Fatal(http.ListenAndServe(":8200", mux))
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Stream   bool `json:"stream"`
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
