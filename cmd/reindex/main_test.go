package main

import (
	"context"
	"testing"

	"open-ima/internal/application/ingest"
	"open-ima/internal/domain/media"
	"open-ima/internal/infrastructure/db"
)

func TestReindexResetsDocumentsAndEnqueuesJobs(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO knowledge_bases (kb_biz_id, name) VALUES ('kb1', 'k')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO medias (media_biz_id, kb_biz_id, title, source_type, source_uri, file_type, status, error)
		VALUES ('d1', 'kb1', 'ready', 'file', 'key1', 'md', 'ready', ''),
		       ('d2', 'kb1', 'failed', 'file', 'key2', 'md', 'failed', 'old error'),
		       ('d3', 'kb1', 'deleting', 'file', 'key3', 'md', 'deleting', '')`); err != nil {
		t.Fatal(err)
	}
	count, err := reindex(context.Background(), database)
	if err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	var pending, jobs, deleting int
	_ = database.QueryRow(`SELECT COUNT(*) FROM medias WHERE status=? AND error=''`, media.StatusPending).Scan(&pending)
	_ = database.QueryRow(`SELECT COUNT(*) FROM jobs WHERE type=? AND status='pending'`, ingest.JobParseDocument).Scan(&jobs)
	_ = database.QueryRow(`SELECT COUNT(*) FROM medias WHERE status=?`, media.StatusDeleting).Scan(&deleting)
	if pending != 2 || jobs != 2 || deleting != 1 {
		t.Fatalf("pending=%d jobs=%d deleting=%d", pending, jobs, deleting)
	}
}
