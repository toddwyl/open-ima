package chat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"open-ima/internal/domain/conversation"
	"open-ima/internal/domain/knowledgebase"
	"open-ima/internal/infrastructure/db"
	"open-ima/internal/infrastructure/llm"
	"open-ima/internal/infrastructure/meili"
)

type chatRig struct {
	service       *Service
	searchQueries []string
	mu            sync.Mutex
}

func newChatRig(t *testing.T) *chatRig {
	t.Helper()
	rig := &chatRig{}
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO knowledge_bases (kb_biz_id, name) VALUES ('kb1', 'Knowledge')`); err != nil {
		t.Fatal(err)
	}

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

	rig.service = NewService(
		conversation.NewConversationService(db.NewConversationRepository(database)),
		knowledgebase.NewKBService(db.NewKnowledgeBaseRepository(database)),
		meili.New(meiliServer.URL, ""), llm.NewChatClient(chatServer.URL, "", "chat"), "chunks",
	)
	return rig
}

func TestSearchHybridAndText(t *testing.T) {
	rig := newChatRig(t)
	results, err := rig.service.Search(context.Background(), "kb1", "alpha", "hybrid")
	if err != nil || len(results) != 2 || results[0].Snippet != "<em>alpha</em>" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	if _, err := rig.service.Search(context.Background(), "kb1", "alpha", "text"); err != nil {
		t.Fatal(err)
	}
}

func TestListMessagesReturnsEmptyArray(t *testing.T) {
	rig := newChatRig(t)
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
	rig := newChatRig(t)
	_, err := rig.service.Search(context.Background(), "missing", "alpha", "hybrid")
	if !errors.Is(err, knowledgebase.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	rig.mu.Lock()
	defer rig.mu.Unlock()
	if len(rig.searchQueries) != 0 {
		t.Fatalf("searches=%v", rig.searchQueries)
	}
}

func TestChatAnswerAndHistory(t *testing.T) {
	rig := newChatRig(t)
	ctx := context.Background()
	var tokens strings.Builder
	conversationID, citations, err := rig.service.Chat(ctx, "kb1", "", "original query", func(token string) error {
		tokens.WriteString(token)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if conversationID == "" || tokens.String() != "Answer [1]" || len(citations) != 3 {
		t.Fatalf("conversation=%s tokens=%q citations=%+v", conversationID, tokens.String(), citations)
	}
	if citations[0].ChunkID != "c2" {
		t.Fatalf("RRF order = %+v", citations)
	}
	messages, err := rig.service.ListMessages(ctx, conversationID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	if messages[0].Role != "user" || messages[1].Content != "Answer [1]" || len(messages[1].Citations) != 3 {
		t.Fatalf("messages=%+v", messages)
	}
	rig.mu.Lock()
	defer rig.mu.Unlock()
	if len(rig.searchQueries) < 2 || rig.searchQueries[len(rig.searchQueries)-2] != "original query" || rig.searchQueries[len(rig.searchQueries)-1] != "expanded query" {
		t.Fatalf("queries = %v", rig.searchQueries)
	}
}

func TestChatRejectsConversationFromAnotherKB(t *testing.T) {
	rig := newChatRig(t)
	ctx := context.Background()
	conversationID, _, err := rig.service.Chat(ctx, "kb1", "", "q", func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = rig.service.Chat(ctx, "other", conversationID, "q", func(string) error { return nil })
	if !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("expected ownership error, got %v", err)
	}
}

func TestChatRejectsUnknownKnowledgeBase(t *testing.T) {
	rig := newChatRig(t)
	_, _, err := rig.service.Chat(context.Background(), "missing", "", "q", func(string) error { return nil })
	if !errors.Is(err, knowledgebase.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}
