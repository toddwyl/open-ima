package sqlite

import (
	"context"
	"errors"
	"testing"

	"open-ima/internal/domain/document"
)

func newDocumentService(t *testing.T) (*document.Service, context.Context) {
	t.Helper()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO knowledge_bases (id, name) VALUES ('kb1', '测试库')`); err != nil {
		t.Fatal(err)
	}
	return document.NewService(NewDocumentRepository(database)), context.Background()
}

func TestDocumentCreateDedupesByHash(t *testing.T) {
	svc, ctx := newDocumentService(t)
	id1, duplicate1, err := svc.Create(ctx, "kb1", "a.md", "file", "hash-key", "md", "hash-key")
	if err != nil || duplicate1 {
		t.Fatalf("first create: duplicate=%v err=%v", duplicate1, err)
	}
	id2, duplicate2, err := svc.Create(ctx, "kb1", "b.md", "file", "hash-key", "md", "hash-key")
	if err != nil || !duplicate2 || id1 != id2 {
		t.Fatalf("dedupe: id1=%s id2=%s duplicate=%v err=%v", id1, id2, duplicate2, err)
	}
	doc, err := svc.Get(ctx, id1)
	if err != nil || doc.Status != document.StatusPending {
		t.Fatalf("doc = %+v err=%v", doc, err)
	}
}

func TestDocumentRetryRules(t *testing.T) {
	svc, ctx := newDocumentService(t)
	if err := svc.Retry(ctx, "missing"); !errors.Is(err, document.ErrNotFound) {
		t.Fatalf("missing document error = %v", err)
	}
	id, _, err := svc.Create(ctx, "kb1", "a.md", "file", "k1", "md", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Retry(ctx, id); !errors.Is(err, document.ErrNotFailed) {
		t.Fatalf("pending document error = %v", err)
	}
	if err := svc.MarkFailed(ctx, id, errors.New("broken")); err != nil {
		t.Fatal(err)
	}
	if err := svc.Retry(ctx, id); err != nil {
		t.Fatal(err)
	}
	doc, _ := svc.Get(ctx, id)
	if doc.Status != document.StatusPending || doc.Error != "" {
		t.Fatalf("doc after retry = %+v", doc)
	}
}

func TestDocumentDeleteRules(t *testing.T) {
	svc, ctx := newDocumentService(t)
	if err := svc.BeginDelete(ctx, "missing"); !errors.Is(err, document.ErrNotFound) {
		t.Fatalf("missing document error = %v", err)
	}
	id, _, err := svc.Create(ctx, "kb1", "a.md", "file", "k1", "md", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ReplaceChunks(ctx, id, []document.StoredChunk{{ID: "c1", Seq: 0, TokenCount: 3}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.BeginDelete(ctx, id); err != nil {
		t.Fatal(err)
	}
	doc, _ := svc.Get(ctx, id)
	if doc.Status != document.StatusDeleting {
		t.Fatalf("doc = %+v", doc)
	}
	if err := svc.BeginDelete(ctx, id); !errors.Is(err, document.ErrDeleting) {
		t.Fatalf("deleting document error = %v", err)
	}
	ids, err := svc.DeletingIDs(ctx)
	if err != nil || len(ids) != 1 || ids[0] != id {
		t.Fatalf("deleting ids = %v err=%v", ids, err)
	}
	if err := svc.FinalizeDelete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, id); !errors.Is(err, document.ErrNotFound) {
		t.Fatalf("get after delete = %v", err)
	}
}

func TestDocumentReplaceChunksClearsPrevious(t *testing.T) {
	svc, ctx := newDocumentService(t)
	id, _, err := svc.Create(ctx, "kb1", "a.md", "file", "k1", "md", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ReplaceChunks(ctx, id, []document.StoredChunk{{ID: "c1", Seq: 0}, {ID: "c2", Seq: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReplaceChunks(ctx, id, []document.StoredChunk{{ID: "c3", Seq: 0}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkReady(ctx, id, 1); err != nil {
		t.Fatal(err)
	}
	doc, _ := svc.Get(ctx, id)
	if doc.Status != document.StatusReady || doc.ChunkCount != 1 {
		t.Fatalf("doc = %+v", doc)
	}
}
