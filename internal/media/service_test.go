package media

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"open-ima/internal/chunker"
	"open-ima/internal/db"
	"open-ima/internal/meili"
	"open-ima/internal/parserclient"
	"open-ima/internal/queue"
	"open-ima/internal/storage"
)

type testRig struct {
	db         *sql.DB
	svc        *Service
	q          *queue.Queue
	w          *queue.Worker
	parserSrv  *httptest.Server
	meiliDocs  [][]byte
	meiliDels  []string
	mu         sync.Mutex
	parserCode int
	parserBody string
}

func newRig(t *testing.T) *testRig {
	t.Helper()
	rig := &testRig{
		parserCode: http.StatusOK,
		parserBody: `{"title":"doc","blocks":[{"type":"heading","text":"H1","level":1},{"type":"paragraph","text":"正文内容"}]}`,
	}
	rig.parserSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rig.mu.Lock()
		code, body := rig.parserCode, rig.parserBody
		rig.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(rig.parserSrv.Close)

	meiliServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/documents") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			rig.meiliDocs = append(rig.meiliDocs, body)
		case strings.HasSuffix(r.URL.Path, "/documents/delete"):
			var body struct {
				Filter string `json:"filter"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			rig.meiliDels = append(rig.meiliDels, body.Filter)
		case strings.HasPrefix(r.URL.Path, "/tasks/"):
			_, _ = io.WriteString(w, `{"status":"succeeded"}`)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"taskUid":1}`)
	}))
	t.Cleanup(meiliServer.Close)

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO knowledge_bases (id, name) VALUES ('kb1', '测试库')`); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocalStorage(filepath.Join(t.TempDir(), "files"), "http://app:8080", "secret")
	if err != nil {
		t.Fatal(err)
	}
	jobQueue := queue.New(database)
	jobQueue.Backoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	meiliClient := meili.New(meiliServer.URL, "")
	meiliClient.PollInterval = time.Millisecond
	service := NewService(Deps{
		DB: database, Store: store, Queue: jobQueue,
		Parser: parserclient.New(rig.parserSrv.URL),
		Meili:  meiliClient, Chunker: chunker.New(512, 80), MeiliIndex: "chunks",
	})
	worker := queue.NewWorker(jobQueue)
	service.RegisterHandlers(worker)
	rig.db, rig.svc, rig.q, rig.w = database, service, jobQueue, worker
	return rig
}

func seedFile(t *testing.T, rig *testRig, content string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	key := hex.EncodeToString(sum[:])
	if err := rig.svc.deps.Store.Put(context.Background(), key, strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	return key
}

func (rig *testRig) drainJobs(ctx context.Context) {
	for rig.w.RunOnce(ctx) {
	}
}

func TestParsePipelineToReady(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	key := seedFile(t, rig, "# 你好")
	documentID, duplicate, err := rig.svc.CreateDocument(ctx, "kb1", "你好.md", "file", key, "md", key)
	if err != nil || duplicate {
		t.Fatalf("create: duplicate=%v err=%v", duplicate, err)
	}
	rig.drainJobs(ctx)
	document, err := rig.svc.Get(ctx, documentID)
	if err != nil {
		t.Fatal(err)
	}
	if document.Status != StatusReady || document.ChunkCount != 1 || document.Error != "" {
		t.Fatalf("document = %+v", document)
	}
	var chunkRows int
	_ = rig.db.QueryRow(`SELECT COUNT(*) FROM chunks WHERE document_id=?`, documentID).Scan(&chunkRows)
	if chunkRows != 1 {
		t.Fatalf("chunk rows = %d", chunkRows)
	}
	if len(rig.meiliDocs) != 1 {
		t.Fatalf("meili add calls = %d", len(rig.meiliDocs))
	}
	var posted []map[string]any
	_ = json.Unmarshal(rig.meiliDocs[0], &posted)
	if posted[0]["kb_id"] != "kb1" || posted[0]["document_id"] != documentID {
		t.Fatalf("meili document = %v", posted[0])
	}
	if _, exists := posted[0]["_vectors"]; exists {
		t.Fatalf("Meilisearch-managed document contains _vectors: %v", posted[0])
	}
}

func TestHashDedup(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	key := seedFile(t, rig, "same content")
	id1, duplicate1, _ := rig.svc.CreateDocument(ctx, "kb1", "a.md", "file", key, "md", key)
	id2, duplicate2, err := rig.svc.CreateDocument(ctx, "kb1", "b.md", "file", key, "md", key)
	if err != nil || !duplicate2 || id1 != id2 || duplicate1 {
		t.Fatalf("id1=%s id2=%s duplicate1=%v duplicate2=%v err=%v", id1, id2, duplicate1, duplicate2, err)
	}
	var jobs int
	_ = rig.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE type=?`, JobParseDocument).Scan(&jobs)
	if jobs != 1 {
		t.Fatalf("jobs = %d, want 1", jobs)
	}
}

func TestCreateDocumentRejectsUnknownKnowledgeBase(t *testing.T) {
	rig := newRig(t)
	key := seedFile(t, rig, "orphan")
	_, _, err := rig.svc.CreateDocument(context.Background(), "missing", "orphan.md", "file", key, "md", key)
	if !errors.Is(err, ErrKnowledgeBaseNotFound) {
		t.Fatalf("err = %v", err)
	}
	var documents, jobs int
	_ = rig.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE kb_id='missing'`).Scan(&documents)
	_ = rig.db.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&jobs)
	if documents != 0 || jobs != 0 {
		t.Fatalf("documents=%d jobs=%d", documents, jobs)
	}
}

func TestParser422FailsDocumentWithoutRetry(t *testing.T) {
	rig := newRig(t)
	rig.parserCode = http.StatusUnprocessableEntity
	rig.parserBody = `{"error":"pdf: encrypted"}`
	ctx := context.Background()
	key := seedFile(t, rig, "x")
	documentID, _, _ := rig.svc.CreateDocument(ctx, "kb1", "x.pdf", "file", key, "pdf", key)
	rig.drainJobs(ctx)
	document, _ := rig.svc.Get(ctx, documentID)
	if document.Status != StatusFailed || !strings.Contains(document.Error, "encrypted") {
		t.Fatalf("document = %+v", document)
	}
	var pending int
	_ = rig.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE status IN ('pending','running')`).Scan(&pending)
	if pending != 0 {
		t.Fatalf("should not retry: pending = %d", pending)
	}
}

func TestRetryableErrorExhaustionMarksFailed(t *testing.T) {
	rig := newRig(t)
	rig.parserCode = http.StatusBadGateway
	rig.parserBody = `{"error":"fetch failed"}`
	ctx := context.Background()
	key := seedFile(t, rig, "x")
	documentID, _, _ := rig.svc.CreateDocument(ctx, "kb1", "x.md", "file", key, "md", key)
	for range 5 {
		time.Sleep(5 * time.Millisecond)
		rig.drainJobs(ctx)
	}
	document, _ := rig.svc.Get(ctx, documentID)
	if document.Status != StatusFailed || document.Error == "" {
		t.Fatalf("document = %+v", document)
	}
}

func TestRetryRequeuesFailedDocument(t *testing.T) {
	rig := newRig(t)
	rig.parserCode = http.StatusUnprocessableEntity
	rig.parserBody = `{"error":"broken"}`
	ctx := context.Background()
	key := seedFile(t, rig, "x")
	documentID, _, _ := rig.svc.CreateDocument(ctx, "kb1", "x.md", "file", key, "md", key)
	rig.drainJobs(ctx)
	rig.mu.Lock()
	rig.parserCode = http.StatusOK
	rig.parserBody = `{"title":"doc","blocks":[{"type":"paragraph","text":"恢复"}]}`
	rig.mu.Unlock()
	if err := rig.svc.Retry(ctx, documentID); err != nil {
		t.Fatal(err)
	}
	rig.drainJobs(ctx)
	document, _ := rig.svc.Get(ctx, documentID)
	if document.Status != StatusReady || document.Error != "" {
		t.Fatalf("document = %+v", document)
	}
}

func TestRetryRejectsNonFailed(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	key := seedFile(t, rig, "x")
	documentID, _, _ := rig.svc.CreateDocument(ctx, "kb1", "x.md", "file", key, "md", key)
	if err := rig.svc.Retry(ctx, documentID); !errors.Is(err, ErrDocumentNotFailed) {
		t.Fatalf("pending document error = %v", err)
	}
	if err := rig.svc.Retry(ctx, "missing"); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("missing document error = %v", err)
	}
}

func TestDeleteReportsMissingAndDeleting(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	if err := rig.svc.Delete(ctx, "missing"); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("missing document error = %v", err)
	}
	key := seedFile(t, rig, "# deleting")
	documentID, _, _ := rig.svc.CreateDocument(ctx, "kb1", "deleting.md", "file", key, "md", key)
	if err := rig.svc.Delete(ctx, documentID); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.Delete(ctx, documentID); !errors.Is(err, ErrDocumentDeleting) {
		t.Fatalf("deleting document error = %v", err)
	}
}

func TestDeleteFlowAndReconcile(t *testing.T) {
	rig := newRig(t)
	ctx := context.Background()
	key := seedFile(t, rig, "# 你好")
	documentID, _, _ := rig.svc.CreateDocument(ctx, "kb1", "你好.md", "file", key, "md", key)
	rig.drainJobs(ctx)
	if err := rig.svc.Delete(ctx, documentID); err != nil {
		t.Fatal(err)
	}
	document, err := rig.svc.Get(ctx, documentID)
	if err != nil || document.Status != StatusDeleting {
		t.Fatalf("document = %+v err=%v", document, err)
	}
	var chunkRows int
	_ = rig.db.QueryRow(`SELECT COUNT(*) FROM chunks WHERE document_id=?`, documentID).Scan(&chunkRows)
	if chunkRows != 0 {
		t.Fatalf("chunks should be cleared at delete request: %d", chunkRows)
	}
	rig.drainJobs(ctx)
	if _, err := rig.svc.Get(ctx, documentID); err == nil {
		t.Fatal("document row should be gone")
	}
	wantFilter := fmt.Sprintf("document_id = '%s'", documentID)
	found := false
	for _, filter := range rig.meiliDels {
		if filter == wantFilter {
			found = true
		}
	}
	if !found {
		t.Fatalf("meili delete filters = %v", rig.meiliDels)
	}
	if _, err := rig.svc.deps.Store.Get(ctx, key); err == nil {
		t.Fatal("storage file should be deleted")
	}

	stuckID := "stuck-doc"
	_, err = rig.db.Exec(
		`INSERT INTO documents (id, kb_id, title, source_type, source_uri, file_type, status) VALUES (?, 'kb1', 's', 'file', ?, 'md', 'deleting')`,
		stuckID, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.EnqueueReconcile(ctx); err != nil {
		t.Fatal(err)
	}
	rig.drainJobs(ctx)
	rig.drainJobs(ctx)
	var count int
	_ = rig.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE id=?`, stuckID).Scan(&count)
	if count != 0 {
		t.Fatal("reconcile should clean stuck deleting document")
	}
}
