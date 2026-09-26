package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"open-ima/internal/db"
	"open-ima/internal/llm"
	"open-ima/internal/meili"
)

type ragRig struct {
	service        *Service
	embeddingCalls int
	searchQueries  []string
	mu             sync.Mutex
}

func newRAGRig(t *testing.T) *ragRig {
	t.Helper()
	rig := &ragRig{}
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO knowledge_bases (id, name) VALUES ('kb1', 'Knowledge')`); err != nil {
		t.Fatal(err)
	}

	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		rig.mu.Lock()
		rig.embeddingCalls++
		rig.mu.Unlock()
		data := make([]map[string]any, len(request.Input))
		for index := range request.Input {
			data[index] = map[string]any{"index": index, "embedding": []float64{float64(index), 0.2}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(embeddingServer.Close)

	meiliServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		query, _ := request["q"].(string)
		rig.mu.Lock()
		rig.searchQueries = append(rig.searchQueries, query)
		rig.mu.Unlock()
		hits := []map[string]any{
			{"id": "c1", "kb_id": "kb1", "document_id": "d1", "title": "One", "content": "alpha", "_formatted": map[string]any{"content": "<em>alpha</em>"}, "_rankingScore": 0.9},
			{"id": "c2", "kb_id": "kb1", "document_id": "d2", "title": "Two", "content": "shared", "_formatted": map[string]any{"content": "shared"}, "_rankingScore": 0.8},
		}
		if query == "expanded query" {
			hits = []map[string]any{
				{"id": "c2", "kb_id": "kb1", "document_id": "d2", "title": "Two", "content": "shared", "_formatted": map[string]any{"content": "shared"}, "_rankingScore": 0.95},
				{"id": "c3", "kb_id": "kb1", "document_id": "d3", "title": "Three", "content": "gamma", "_formatted": map[string]any{"content": "gamma"}, "_rankingScore": 0.7},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": hits})
	}))
	t.Cleanup(meiliServer.Close)

	chatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if !request.Stream {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"expanded query"}}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Answer \"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"[1]\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(chatServer.Close)

	rig.service = NewService(Deps{
		DB: database, Meili: meili.New(meiliServer.URL, ""),
		Chat: llm.NewChatClient(chatServer.URL, "", "chat"), MeiliIndex: "chunks",
	})
	return rig
}

func TestSearchHybridAndText(t *testing.T) {
	rig := newRAGRig(t)
	results, err := rig.service.Search(context.Background(), "kb1", "alpha", "hybrid")
	if err != nil || len(results) != 2 || results[0].Snippet != "<em>alpha</em>" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	if _, err := rig.service.Search(context.Background(), "kb1", "alpha", "text"); err != nil {
		t.Fatal(err)
	}
	rig.mu.Lock()
	defer rig.mu.Unlock()
}

func TestListMessagesReturnsEmptyArray(t *testing.T) {
	rig := newRAGRig(t)
	messages, err := rig.service.ListMessages(context.Background(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("messages JSON = %s", encoded)
	}
}

func TestSearchRejectsUnknownKnowledgeBaseBeforeExternalCalls(t *testing.T) {
	rig := newRAGRig(t)
	_, err := rig.service.Search(context.Background(), "missing", "alpha", "hybrid")
	if !errors.Is(err, ErrKnowledgeBaseNotFound) {
		t.Fatalf("err = %v", err)
	}
	rig.mu.Lock()
	defer rig.mu.Unlock()
	if rig.embeddingCalls != 0 || len(rig.searchQueries) != 0 {
		t.Fatalf("embedding=%d searches=%v", rig.embeddingCalls, rig.searchQueries)
	}
}

func TestChatSSEAndHistory(t *testing.T) {
	rig := newRAGRig(t)
	mux := http.NewServeMux()
	rig.service.RegisterRoutes(mux)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/kbs/kb1/chat", bytes.NewBufferString(`{"query":"original query"}`))
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, event := range []string{"event: token", "event: citations", "event: done", "Answer ", "[1]"} {
		if !strings.Contains(body, event) {
			t.Fatalf("missing %q in %s", event, body)
		}
	}
	if strings.Index(body, "event: token") > strings.Index(body, "event: citations") {
		t.Fatalf("citations arrived before tokens: %s", body)
	}
	var conversationID string
	for _, block := range strings.Split(body, "\n\n") {
		if strings.HasPrefix(block, "event: done") {
			var done map[string]string
			_ = json.Unmarshal([]byte(strings.TrimPrefix(strings.Split(block, "\n")[1], "data: ")), &done)
			conversationID = done["conversation_id"]
		}
	}
	if conversationID == "" {
		t.Fatal("missing conversation id")
	}
	messages, err := rig.service.ListMessages(context.Background(), conversationID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	if messages[0].Role != "user" || messages[1].Content != "Answer [1]" || len(messages[1].Citations) != 3 {
		t.Fatalf("messages=%+v", messages)
	}
	if messages[1].Citations[0].ChunkID != "c2" {
		t.Fatalf("RRF order = %+v", messages[1].Citations)
	}
	rig.mu.Lock()
	defer rig.mu.Unlock()
	if len(rig.searchQueries) < 2 || rig.searchQueries[len(rig.searchQueries)-2] != "original query" || rig.searchQueries[len(rig.searchQueries)-1] != "expanded query" {
		t.Fatalf("queries = %v", rig.searchQueries)
	}
}

func TestChatRejectsConversationFromAnotherKB(t *testing.T) {
	rig := newRAGRig(t)
	conversation, err := rig.service.ensureConversation(context.Background(), "kb1", "", "q")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = rig.service.Chat(context.Background(), "other", conversation.ID, "q", func(string) error { return nil })
	if err == nil {
		t.Fatal("expected ownership error")
	}
}
