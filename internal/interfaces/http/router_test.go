package httpapi

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"open-ima/internal/application/chat"
	"open-ima/internal/application/ingest"
	kbapp "open-ima/internal/application/knowledgebase"
	"open-ima/internal/application/port"
	settingsapp "open-ima/internal/application/settings"
	"open-ima/internal/domain/conversation"
	"open-ima/internal/domain/media"
	kbdom "open-ima/internal/domain/knowledgebase"
	settingsdom "open-ima/internal/domain/settings"
	"open-ima/internal/infrastructure/db"
	"open-ima/internal/infrastructure/fetch"
	"open-ima/internal/infrastructure/llm"
	"open-ima/internal/infrastructure/meili"
	"open-ima/internal/infrastructure/parser"
	"open-ima/internal/infrastructure/queue"
	"open-ima/internal/infrastructure/storage"
)

type testServices struct {
	kb            *kbapp.Service
	ingest        *ingest.Service
	chat          *chat.Service
	settings      *settingsapp.Service
	store         *storage.LocalStorage
	database      *sql.DB
	searchQueries []string
	mu            sync.Mutex
}

func newTestServices(t *testing.T) *testServices {
	t.Helper()
	services := &testServices{}
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO knowledge_bases (kb_biz_id, name) VALUES ('kb1', 'k')`); err != nil {
		t.Fatal(err)
	}

	meiliServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && len(r.URL.Path) > 7 && r.URL.Path[len(r.URL.Path)-7:] == "/search" {
			var request map[string]any
			_ = json.NewDecoder(r.Body).Decode(&request)
			query, _ := request["q"].(string)
			services.mu.Lock()
			services.searchQueries = append(services.searchQueries, query)
			services.mu.Unlock()
			hits := []map[string]any{
				{"id": "c1", "kb_biz_id": "kb1", "document_biz_id": "d1", "title": "One", "content": "alpha", "_formatted": map[string]any{"content": "<em>alpha</em>"}, "_rankingScore": 0.9},
				{"id": "c2", "kb_biz_id": "kb1", "document_biz_id": "d2", "title": "Two", "content": "shared", "_formatted": map[string]any{"content": "shared"}, "_rankingScore": 0.8},
			}
			if query == "expanded query" {
				hits = []map[string]any{
					{"id": "c2", "kb_biz_id": "kb1", "document_biz_id": "d2", "title": "Two", "content": "shared", "_formatted": map[string]any{"content": "shared"}, "_rankingScore": 0.95},
					{"id": "c3", "kb_biz_id": "kb1", "document_biz_id": "d3", "title": "Three", "content": "gamma", "_formatted": map[string]any{"content": "gamma"}, "_rankingScore": 0.7},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hits": hits})
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"taskUid":1}`)
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

	parserStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(parserStub.Close)

	store, err := storage.NewLocalStorage(filepath.Join(t.TempDir(), "files"), "http://app:8080", "s")
	if err != nil {
		t.Fatal(err)
	}
	kbs := kbdom.NewKBService(db.NewKnowledgeBaseRepository(database))
	docs := document.NewDocumentService(db.NewDocumentRepository(database))
	conversations := conversation.NewConversationService(db.NewConversationRepository(database))
	meiliClient := meili.New(meiliServer.URL, "")

	ingestService := ingest.NewService(
		docs, kbs, queue.New(database), store,
		parser.New(parserStub.URL), meiliClient, document.NewChunker(512, 80), "chunks",
	)
	chatService := chat.NewService(
		conversations, kbs, meiliClient, llm.NewChatClient(chatServer.URL, "", "chat"), "chunks",
	)
	settingsService := settingsapp.NewService(
		db.NewSettingsRepository(database), settingsdom.NewSettingsService(), meiliClient, "chunks", chatService,
		func(protocol, baseURL, apiKey, model string) port.ChatModel {
			return llm.NewChatClientWithProtocol(protocol, baseURL, apiKey, model)
		},
		settingsdom.Values{ChatModels: []settingsdom.ChatModel{{ModelBizID: "default"}}, DefaultChatModelBizID: "default", EmbedderDimensions: 1024},
	)

	services.kb = kbapp.NewService(kbs, docs, conversations, ingestService, store, fetch.New())
	services.ingest = ingestService
	services.chat = chatService
	services.settings = settingsService
	services.store = store
	services.database = database
	return services
}

func (s *testServices) router() *http.ServeMux {
	return NewRouter(Deps{
		KnowledgeBase: s.kb, Ingest: s.ingest, Chat: s.chat, Settings: s.settings, Store: s.store,
	})
}
