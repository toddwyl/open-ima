package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"open-ima/internal/application/reading"
	"open-ima/internal/domain/media"
	"open-ima/internal/infrastructure/db"
	"open-ima/internal/infrastructure/meili"
	"open-ima/internal/infrastructure/storage"
)

type recordingOpener struct{ called bool }

func (o *recordingOpener) Open(_ context.Context, _ string) error {
	o.called = true
	return nil
}

// newReadingTestMux 装配只含阅读路由的 mux，用真实 SQLite + 假 Meilisearch。
func newReadingTestMux(t *testing.T) (mux *http.ServeMux, opener *recordingOpener, fileDoc, urlDoc string) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO knowledge_bases (kb_biz_id, name) VALUES ('kb1', 'Knowledge')`); err != nil {
		t.Fatal(err)
	}
	meiliServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits := []map[string]any{{"id": "chunk-a", "content": "first"}}
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": hits})
	}))
	t.Cleanup(meiliServer.Close)
	store, err := storage.NewLocalStorage(t.TempDir(), "http://127.0.0.1", "secret")
	if err != nil {
		t.Fatal(err)
	}
	opener = &recordingOpener{}
	repo := db.NewDocumentRepository(database)
	service := reading.NewService(media.NewDocumentService(repo), meili.New(meiliServer.URL, ""), store, opener, "chunks")
	mux = http.NewServeMux()
	(&readingHandler{reading: service}).register(mux)

	docSvc := media.NewDocumentService(repo)
	create := func(sourceType, fileType string) string {
		id, _, err := docSvc.Create(context.Background(), "kb1", "产业笔记", sourceType, "uri", fileType, "hash-"+sourceType+fileType)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	fileDoc = create("file", "md")
	urlDoc = create("url", "html")
	if err := repo.ReplaceChunks(context.Background(), fileDoc, []media.StoredChunk{{BizID: "chunk-a", Seq: 1}}); err != nil {
		t.Fatal(err)
	}
	return mux, opener, fileDoc, urlDoc
}

func TestDocumentContentEndpoint(t *testing.T) {
	mux, _, fileDoc, _ := newReadingTestMux(t)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/documents/"+fileDoc+"/content", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Title  string `json:"title"`
		Chunks []struct {
			ChunkBizID string `json:"chunk_biz_id"`
			Content    string `json:"content"`
		} `json:"chunks"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Title != "产业笔记" || len(body.Chunks) != 1 || body.Chunks[0].Content != "first" {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
}

func TestDocumentContentEndpointNotFoundAndConflict(t *testing.T) {
	mux, _, _, urlDoc := newReadingTestMux(t)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/documents/missing/content", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/documents/"+urlDoc+"/content", nil))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected 409 for unindexed document, got %d", recorder.Code)
	}
}

func TestDocumentOpenEndpoint(t *testing.T) {
	mux, opener, _, urlDoc := newReadingTestMux(t)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/documents/missing/open", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/documents/"+urlDoc+"/open", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for url source, got %d", recorder.Code)
	}
	if opener.called {
		t.Fatal("opener must not be called for url sources")
	}
}
