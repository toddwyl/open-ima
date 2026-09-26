package kb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"open-ima/internal/chunker"
	"open-ima/internal/db"
	"open-ima/internal/llm"
	"open-ima/internal/media"
	"open-ima/internal/meili"
	"open-ima/internal/parserclient"
	"open-ima/internal/queue"
	"open-ima/internal/storage"
)

func newMediaForKB(t *testing.T, database *sql.DB) (*media.Service, storage.Storage) {
	t.Helper()
	store, err := storage.NewLocalStorage(filepath.Join(t.TempDir(), "files"), "http://app:8080", "s")
	if err != nil {
		t.Fatal(err)
	}
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(stub.Close)
	service := media.NewService(media.Deps{
		DB: database, Store: store, Queue: queue.New(database),
		Parser: parserclient.New(stub.URL), Embedder: llm.NewEmbeddingClient(stub.URL, "", "m"),
		Meili: meili.New(stub.URL, ""), Chunker: chunker.New(512, 80), MeiliIndex: "chunks",
	})
	return service, store
}

func newKBService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	mediaService, store := newMediaForKB(t, database)
	return NewService(database, mediaService, store), database
}

func TestCreateListDeleteKB(t *testing.T) {
	service, database := newKBService(t)
	ctx := context.Background()
	knowledgeBase, err := service.Create(ctx, "工作笔记", "描述")
	if err != nil || knowledgeBase.ID == "" {
		t.Fatalf("create: %v", err)
	}
	if _, err := service.Create(ctx, "工作笔记", ""); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("duplicate name err = %v", err)
	}
	list, err := service.List(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "工作笔记" || list[0].DocCount != 0 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	sum := sha256.Sum256([]byte("x"))
	hash := hex.EncodeToString(sum[:])
	_, err = database.Exec(
		`INSERT INTO documents (id, kb_id, title, source_type, source_uri, file_type, file_hash) VALUES ('d1', ?, 't', 'file', ?, 'md', ?)`,
		knowledgeBase.ID, hash, hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, knowledgeBase.ID); err != nil {
		t.Fatal(err)
	}
	var kbCount, deletingDocuments int
	_ = database.QueryRow(`SELECT COUNT(*) FROM knowledge_bases WHERE id=?`, knowledgeBase.ID).Scan(&kbCount)
	_ = database.QueryRow(`SELECT COUNT(*) FROM documents WHERE kb_id=? AND status='deleting'`, knowledgeBase.ID).Scan(&deletingDocuments)
	if kbCount != 0 || deletingDocuments != 1 {
		t.Fatalf("kb=%d deleting=%d", kbCount, deletingDocuments)
	}
}

func TestIngestURL(t *testing.T) {
	service, database := newKBService(t)
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body><p>网页正文内容</p></body></html>"))
	}))
	defer page.Close()
	documentID, duplicate, err := service.IngestURL(context.Background(), "kb9", page.URL+"/articles/hello")
	if err != nil || duplicate {
		t.Fatalf("ingest: duplicate=%v err=%v", duplicate, err)
	}
	var title, sourceType, fileType, hash string
	err = database.QueryRow(`SELECT title, source_type, file_type, file_hash FROM documents WHERE id=?`, documentID).
		Scan(&title, &sourceType, &fileType, &hash)
	if err != nil {
		t.Fatal(err)
	}
	if title != "hello" || sourceType != "url" || fileType != "html" || len(hash) != 64 {
		t.Fatalf("document: title=%q source=%q type=%q hash=%q", title, sourceType, fileType, hash)
	}
	_, duplicate, err = service.IngestURL(context.Background(), "kb9", page.URL+"/articles/hello")
	if err != nil || !duplicate {
		t.Fatalf("re-ingest: duplicate=%v err=%v", duplicate, err)
	}
}

func TestIngestURLRejectsBadStatusAndOversize(t *testing.T) {
	service, _ := newKBService(t)
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			http.Error(w, "no", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("x", maxURLBytes+1)))
	}))
	defer page.Close()
	if _, _, err := service.IngestURL(context.Background(), "kb", page.URL+"/bad"); err == nil {
		t.Fatal("expected bad status error")
	}
	if _, _, err := service.IngestURL(context.Background(), "kb", page.URL+"/large"); err == nil || !strings.Contains(err.Error(), "10MB") {
		t.Fatalf("oversize err = %v", err)
	}
}

func TestKBHandlers(t *testing.T) {
	service, _ := newKBService(t)
	mux := http.NewServeMux()
	service.RegisterRoutes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/kbs", strings.NewReader(`{"name":"API库","description":"d"}`)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", recorder.Code, recorder.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &created)
	knowledgeBaseID := created["id"].(string)
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/kbs", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "API库") {
		t.Fatalf("list: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/kbs/"+knowledgeBaseID, nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", recorder.Code)
	}
}
