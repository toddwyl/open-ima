package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
)

type state struct {
	mu      sync.Mutex
	created bool
	docs    []map[string]any
}

func main() {
	s := &state{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "available"})
	})
	mux.HandleFunc("PATCH /experimental-features", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]bool{"vectorStore": true})
	})
	mux.HandleFunc("GET /indexes/{uid}", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		created := s.created
		s.mu.Unlock()
		if !created {
			writeJSON(w, 404, map[string]string{"message": "not found"})
			return
		}
		writeJSON(w, 200, map[string]string{"uid": "chunks", "primaryKey": "id"})
	})
	mux.HandleFunc("POST /indexes", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		s.created = true
		s.mu.Unlock()
		task(w)
	})
	mux.HandleFunc("PATCH /indexes/{uid}/settings", func(w http.ResponseWriter, _ *http.Request) { task(w) })
	mux.HandleFunc("GET /tasks/{uid}", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "succeeded"})
	})
	mux.HandleFunc("POST /indexes/{uid}/documents", func(w http.ResponseWriter, r *http.Request) {
		var docs []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&docs); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		s.mu.Lock()
		s.docs = append(s.docs, docs...)
		s.mu.Unlock()
		task(w)
	})
	mux.HandleFunc("POST /indexes/{uid}/documents/delete", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Filter string `json:"filter"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		parts := strings.Split(request.Filter, "'")
		if len(parts) >= 2 {
			id := parts[1]
			s.mu.Lock()
			filtered := s.docs[:0]
			for _, doc := range s.docs {
				if doc["document_biz_id"] != id {
					filtered = append(filtered, doc)
				}
			}
			s.docs = filtered
			s.mu.Unlock()
		}
		task(w)
	})
	mux.HandleFunc("POST /indexes/{uid}/search", func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		s.mu.Lock()
		docs := append([]map[string]any(nil), s.docs...)
		s.mu.Unlock()
		hits := make([]map[string]any, 0, len(docs))
		for _, doc := range docs {
			hit := make(map[string]any, len(doc)+2)
			for key, value := range doc {
				hit[key] = value
			}
			hit["_formatted"] = map[string]any{"content": doc["content"]}
			hit["_rankingScore"] = 0.9
			hits = append(hits, hit)
		}
		writeJSON(w, 200, map[string]any{"hits": hits})
	})
	log.Printf("mock meilisearch listening on :7700")
	log.Fatal(http.ListenAndServe(":7700", mux))
}

func task(w http.ResponseWriter) { writeJSON(w, 202, map[string]int{"taskUid": 1}) }
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
