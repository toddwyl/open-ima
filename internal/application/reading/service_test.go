package reading

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"open-ima/internal/domain/media"
	"open-ima/internal/infrastructure/db"
	"open-ima/internal/infrastructure/meili"
	"open-ima/internal/infrastructure/storage"
)

type fakeOpener struct{ paths []string }

func (f *fakeOpener) Open(_ context.Context, path string) error {
	f.paths = append(f.paths, path)
	return nil
}

type readingRig struct {
	service *Service
	opener  *fakeOpener
	docSvc  *media.DocumentService
	repo    *db.DocumentRepository
	store   *storage.LocalStorage
}

func newReadingRig(t *testing.T) *readingRig {
	t.Helper()
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
		hits := []map[string]any{
			{"id": "chunk-b", "content": "second"},
			{"id": "chunk-a", "content": "first"},
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": hits})
	}))
	t.Cleanup(meiliServer.Close)
	meiliClient := meili.New(meiliServer.URL, "")
	store, err := storage.NewLocalStorage(t.TempDir(), "http://127.0.0.1", "secret")
	if err != nil {
		t.Fatal(err)
	}
	opener := &fakeOpener{}
	repo := db.NewDocumentRepository(database)
	rig := &readingRig{
		opener: opener, docSvc: media.NewDocumentService(repo), repo: repo, store: store,
	}
	rig.service = NewService(rig.docSvc, meiliClient, store, opener, "chunks")
	return rig
}

func (rig *readingRig) createDocument(t *testing.T, sourceType, fileType string) string {
	t.Helper()
	id, _, err := rig.docSvc.Create(context.Background(), "kb1", "产业笔记", sourceType, blobKey, fileType, "hash-"+fileType)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// blobKey 是符合 FileStore 寻址规则的 64 位十六进制键。
const blobKey = "aa10b2c3d4e5f60718293a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d"

func TestContentReturnsChunksOrderedBySeq(t *testing.T) {
	rig := newReadingRig(t)
	mediaBizID := rig.createDocument(t, "file", "md")
	if err := rig.repo.ReplaceChunks(context.Background(), mediaBizID, []media.StoredChunk{
		{BizID: "chunk-b", Seq: 2}, {BizID: "chunk-a", Seq: 1},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := rig.service.Content(context.Background(), mediaBizID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "产业笔记" || result.FileType != "md" {
		t.Fatalf("unexpected meta: %+v", result)
	}
	if len(result.Chunks) != 2 || result.Chunks[0].ChunkBizID != "chunk-a" || result.Chunks[0].Content != "first" ||
		result.Chunks[1].ChunkBizID != "chunk-b" || result.Chunks[1].Content != "second" {
		t.Fatalf("chunks not ordered by seq: %+v", result.Chunks)
	}
}

func TestContentRejectsMissingOrUnindexedDocument(t *testing.T) {
	rig := newReadingRig(t)
	if _, err := rig.service.Content(context.Background(), "missing"); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	empty := rig.createDocument(t, "file", "md")
	if _, err := rig.service.Content(context.Background(), empty); !errors.Is(err, ErrNotIndexed) {
		t.Fatalf("expected ErrNotIndexed, got %v", err)
	}
}

func TestOpenCopiesStoredFileWithExtension(t *testing.T) {
	rig := newReadingRig(t)
	if err := rig.store.Put(context.Background(), blobKey, strings.NewReader("blob bytes")); err != nil {
		t.Fatal(err)
	}
	mediaBizID := rig.createDocument(t, "file", "md")

	if err := rig.service.Open(context.Background(), mediaBizID); err != nil {
		t.Fatal(err)
	}
	if len(rig.opener.paths) != 1 {
		t.Fatalf("opener not called: %+v", rig.opener.paths)
	}
	opened := rig.opener.paths[0]
	if filepath.Ext(opened) != ".md" {
		t.Fatalf("expected .md extension, got %q", opened)
	}
	content, err := os.ReadFile(opened)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "blob bytes" {
		t.Fatalf("unexpected temp content: %q", content)
	}
}

func TestOpenRejectsURLSource(t *testing.T) {
	rig := newReadingRig(t)
	mediaBizID := rig.createDocument(t, "url", "html")
	if err := rig.service.Open(context.Background(), mediaBizID); !errors.Is(err, ErrIsURL) {
		t.Fatalf("expected ErrIsURL, got %v", err)
	}
	if len(rig.opener.paths) != 0 {
		t.Fatalf("opener must not be called for url sources: %+v", rig.opener.paths)
	}
}
