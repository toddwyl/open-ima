package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/embeddings", handleEmbeddings)
	mux.HandleFunc("POST /v1/chat/completions", handleChat)
	log.Printf("mock model listening on :8200")
	log.Fatal(http.ListenAndServe(":8200", mux))
}

func handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Input []string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	data := make([]map[string]any, len(request.Input))
	for index, text := range request.Input {
		data[index] = map[string]any{
			"index": index, "embedding": []float32{float32(len([]rune(text))) / 100, 0.2, 0.3},
		}
	}
	writeJSON(w, map[string]any{"data": data})
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Stream bool `json:"stream"`
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
	for _, token := range []string{"Open IMA ", "端到端链路正常。[1]"} {
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
