package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"open-ima/internal/domain/knowledgebase"
	"open-ima/internal/infrastructure/db"
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
			{"id": "c1", "kb_biz_id": "kb1", "media_biz_id": "d1", "title": "One", "content": "alpha", "_formatted": map[string]any{"content": "<em>alpha</em>"}, "_rankingScore": 0.9},
			{"id": "c2", "kb_biz_id": "kb1", "media_biz_id": "d2", "title": "Two", "content": "shared", "_formatted": map[string]any{"content": "shared"}, "_rankingScore": 0.8},
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": hits})
	}))
	t.Cleanup(meiliServer.Close)

	rig.service = NewService(
		knowledgebase.NewKBService(db.NewKnowledgeBaseRepository(database)),
		meili.New(meiliServer.URL, ""), "chunks",
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
