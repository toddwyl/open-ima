package db

import (
	"context"
	"errors"
	"testing"

	"open-ima/internal/domain/media"
)

func newMediaService(t *testing.T) (*media.MediaService, context.Context) {
	t.Helper()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO knowledge_bases (kb_biz_id, name) VALUES ('kb1', '测试库')`); err != nil {
		t.Fatal(err)
	}
	return media.NewMediaService(NewMediaRepository(database)), context.Background()
}

func TestMediaCreateDedupesByHash(t *testing.T) {
	svc, ctx := newMediaService(t)
	id1, duplicate1, err := svc.Create(ctx, "kb1", "a.md", "file", "hash-key", "md", "hash-key")
	if err != nil || duplicate1 {
		t.Fatalf("first create: duplicate=%v err=%v", duplicate1, err)
	}
	id2, duplicate2, err := svc.Create(ctx, "kb1", "b.md", "file", "hash-key", "md", "hash-key")
	if err != nil || !duplicate2 || id1 != id2 {
		t.Fatalf("dedupe: id1=%s id2=%s duplicate=%v err=%v", id1, id2, duplicate2, err)
	}
	doc, err := svc.Get(ctx, id1)
	if err != nil || doc.ID <= 0 || doc.Status != media.StatusPending {
		t.Fatalf("doc = %+v err=%v", doc, err)
	}
}

func TestMediaRetryRules(t *testing.T) {
	svc, ctx := newMediaService(t)
	if err := svc.Retry(ctx, "missing"); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("missing document error = %v", err)
	}
	id, _, err := svc.Create(ctx, "kb1", "a.md", "file", "k1", "md", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Retry(ctx, id); !errors.Is(err, media.ErrNotFailed) {
		t.Fatalf("pending document error = %v", err)
	}
	if err := svc.MarkFailed(ctx, id, errors.New("broken")); err != nil {
		t.Fatal(err)
	}
	if err := svc.Retry(ctx, id); err != nil {
		t.Fatal(err)
	}
	doc, _ := svc.Get(ctx, id)
	if doc.Status != media.StatusPending || doc.Error != "" {
		t.Fatalf("doc after retry = %+v", doc)
	}
}

func TestMediaDeleteRules(t *testing.T) {
	svc, ctx := newMediaService(t)
	if err := svc.BeginDelete(ctx, "missing"); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("missing document error = %v", err)
	}
	id, _, err := svc.Create(ctx, "kb1", "a.md", "file", "k1", "md", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ReplaceChunks(ctx, id, []media.StoredChunk{{BizID: "c1", Seq: 0, TokenCount: 3}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.BeginDelete(ctx, id); err != nil {
		t.Fatal(err)
	}
	doc, _ := svc.Get(ctx, id)
	if doc.Status != media.StatusDeleting {
		t.Fatalf("doc = %+v", doc)
	}
	if err := svc.BeginDelete(ctx, id); !errors.Is(err, media.ErrDeleting) {
		t.Fatalf("deleting document error = %v", err)
	}
	ids, err := svc.DeletingIDs(ctx)
	if err != nil || len(ids) != 1 || ids[0] != id {
		t.Fatalf("deleting ids = %v err=%v", ids, err)
	}
	if err := svc.FinalizeDelete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, id); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("get after delete = %v", err)
	}
}

func TestMediaReplaceChunksClearsPrevious(t *testing.T) {
	svc, ctx := newMediaService(t)
	id, _, err := svc.Create(ctx, "kb1", "a.md", "file", "k1", "md", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ReplaceChunks(ctx, id, []media.StoredChunk{{BizID: "c1", Seq: 0}, {BizID: "c2", Seq: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReplaceChunks(ctx, id, []media.StoredChunk{{BizID: "c3", Seq: 0}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkReady(ctx, id, 1); err != nil {
		t.Fatal(err)
	}
	doc, _ := svc.Get(ctx, id)
	if doc.Status != media.StatusReady || doc.ChunkCount != 1 {
		t.Fatalf("doc = %+v", doc)
	}
}

func TestMediaReconcileCandidates(t *testing.T) {
	svc, ctx := newMediaService(t)
	readyID, _, err := svc.Create(ctx, "kb1", "ready.md", "file", "ready-key", "md", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ReplaceChunks(ctx, readyID, []media.StoredChunk{{BizID: "c-ready", Seq: 0}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkReady(ctx, readyID, 1); err != nil {
		t.Fatal(err)
	}
	failedID, _, err := svc.Create(ctx, "kb1", "failed.md", "file", "failed-key", "md", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkFailed(ctx, failedID, errors.New("broken")); err != nil {
		t.Fatal(err)
	}
	deletingID, _, err := svc.Create(ctx, "kb1", "deleting.md", "file", "deleting-key", "md", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.BeginDelete(ctx, deletingID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Create(ctx, "kb1", "pending.md", "file", "pending-key", "md", ""); err != nil {
		t.Fatal(err)
	}

	docs, err := svc.ReconcileCandidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, doc := range docs {
		got[doc.BizID] = doc.Status
	}
	want := map[string]string{
		readyID:    media.StatusReady,
		failedID:   media.StatusFailed,
		deletingID: media.StatusDeleting,
	}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for id, status := range want {
		if got[id] != status {
			t.Fatalf("candidate %s status = %q, want %q; all=%v", id, got[id], status, got)
		}
	}
}
