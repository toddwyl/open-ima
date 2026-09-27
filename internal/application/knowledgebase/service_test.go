package knowledgebase

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"open-ima/internal/application/ingest"
	"open-ima/internal/domain/conversation"
	kbdom "open-ima/internal/domain/knowledgebase"
	"open-ima/internal/domain/media"
	"open-ima/internal/infrastructure/db"
	"open-ima/internal/infrastructure/fetch"
	"open-ima/internal/infrastructure/meili"
	"open-ima/internal/infrastructure/parser"
	"open-ima/internal/infrastructure/queue"
	"open-ima/internal/infrastructure/storage"
)

func newKBService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	store, err := storage.NewLocalStorage(filepath.Join(t.TempDir(), "files"), "http://app:8080", "s")
	if err != nil {
		t.Fatal(err)
	}
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(stub.Close)
	kbs := kbdom.NewKBService(db.NewKnowledgeBaseRepository(database))
	docs := media.NewMediaService(db.NewMediaRepository(database))
	ingestService := ingest.NewService(
		docs, kbs, queue.New(database), store,
		parser.New(stub.URL), meili.New(stub.URL, ""), media.NewChunker(512, 80), "chunks",
	)
	return NewService(
		kbs, docs, conversation.NewConversationService(db.NewConversationRepository(database)),
		ingestService, store, fetch.New(),
	), database
}

func TestCreateListDeleteKB(t *testing.T) {
	service, database := newKBService(t)
	ctx := context.Background()
	kb, err := service.Create(ctx, "工作笔记", "描述")
	if err != nil || kb.BizID == "" {
		t.Fatalf("create: %v", err)
	}
	if _, err := service.Create(ctx, "工作笔记", ""); !errors.Is(err, kbdom.ErrNameTaken) {
		t.Fatalf("duplicate name err = %v", err)
	}
	list, err := service.List(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "工作笔记" || list[0].MediaCount != 0 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	hash := strings.Repeat("a", 64)
	_, err = database.Exec(
		`INSERT INTO medias (media_biz_id, kb_biz_id, title, source_type, source_uri, file_type, file_hash) VALUES ('d1', ?, 't', 'file', ?, 'md', ?)`,
		kb.BizID, hash, hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, kb.BizID); err != nil {
		t.Fatal(err)
	}
	var kbCount, deletingDocuments int
	_ = database.QueryRow(`SELECT COUNT(*) FROM knowledge_bases WHERE kb_biz_id=?`, kb.BizID).Scan(&kbCount)
	_ = database.QueryRow(`SELECT COUNT(*) FROM medias WHERE kb_biz_id=? AND status='deleting'`, kb.BizID).Scan(&deletingDocuments)
	if kbCount != 0 || deletingDocuments != 1 {
		t.Fatalf("kb=%d deleting=%d", kbCount, deletingDocuments)
	}
	if err := service.Delete(ctx, kb.BizID); !errors.Is(err, kbdom.ErrNotFound) {
		t.Fatalf("re-delete err = %v", err)
	}
}

func TestIngestURL(t *testing.T) {
	service, database := newKBService(t)
	if _, err := database.Exec(`INSERT INTO knowledge_bases (kb_biz_id, name) VALUES ('kb9', 'URL')`); err != nil {
		t.Fatal(err)
	}
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body><p>网页正文内容</p></body></html>"))
	}))
	defer page.Close()
	documentBizID, duplicate, err := service.IngestURL(context.Background(), "kb9", page.URL+"/articles/hello")
	if err != nil || duplicate {
		t.Fatalf("ingest: duplicate=%v err=%v", duplicate, err)
	}
	var title, sourceType, fileType, hash string
	err = database.QueryRow(`SELECT title, source_type, file_type, file_hash FROM medias WHERE media_biz_id=?`, documentBizID).
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
		_, _ = w.Write([]byte(strings.Repeat("x", 10<<20+1)))
	}))
	defer page.Close()
	if _, _, err := service.IngestURL(context.Background(), "kb", page.URL+"/bad"); err == nil {
		t.Fatal("expected bad status error")
	}
	if _, _, err := service.IngestURL(context.Background(), "kb", page.URL+"/large"); err == nil || !strings.Contains(err.Error(), "10MB") {
		t.Fatalf("oversize err = %v", err)
	}
}

func TestIngestURLRejectsInvalidURL(t *testing.T) {
	service, _ := newKBService(t)
	if _, _, err := service.IngestURL(context.Background(), "kb", "file:///etc/passwd"); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("err = %v", err)
	}
}
