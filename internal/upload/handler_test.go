package upload

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"open-ima/internal/chunker"
	"open-ima/internal/infrastructure/meili"
	"open-ima/internal/infrastructure/parser"
	"open-ima/internal/infrastructure/queue"
	"open-ima/internal/infrastructure/sqlite"
	"open-ima/internal/infrastructure/storage"
	"open-ima/internal/media"
)

func newUploadRig(t *testing.T) (*Handler, *sql.DB) {
	t.Helper()
	database, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO knowledge_bases (id, name) VALUES ('kb1', 'k')`); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocalStorage(filepath.Join(t.TempDir(), "files"), "http://app:8080", "s")
	if err != nil {
		t.Fatal(err)
	}
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(stub.Close)
	mediaService := media.NewService(media.Deps{
		DB: database, Store: store, Queue: queue.New(database),
		Parser: parser.New(stub.URL),
		Meili:  meili.New(stub.URL, ""), Chunker: chunker.New(512, 80), MeiliIndex: "chunks",
	})
	return NewHandler(mediaService, store), database
}

func multipartBody(t *testing.T, field, filename, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	file, err := writer.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(file, strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &buffer, writer.FormDataContentType()
}

func TestUploadAcceptedAndDeduplicated(t *testing.T) {
	handler, database := newUploadRig(t)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	body, contentType := multipartBody(t, "file", "笔记.md", "# 标题\n\n正文")
	request := httptest.NewRequest(http.MethodPost, "/api/kbs/kb1/documents", body)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("code = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		DocumentID string `json:"document_id"`
		Duplicate  bool   `json:"duplicate"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.DocumentID == "" || response.Duplicate {
		t.Fatalf("response = %+v err=%v", response, err)
	}
	var status, fileType string
	_ = database.QueryRow(`SELECT status, file_type FROM documents WHERE id=?`, response.DocumentID).Scan(&status, &fileType)
	if status != media.StatusPending || fileType != "md" {
		t.Fatalf("status=%s type=%s", status, fileType)
	}

	body, contentType = multipartBody(t, "file", "改名.md", "# 标题\n\n正文")
	request = httptest.NewRequest(http.MethodPost, "/api/kbs/kb1/documents", body)
	request.Header.Set("Content-Type", contentType)
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	var duplicate struct {
		DocumentID string `json:"document_id"`
		Duplicate  bool   `json:"duplicate"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &duplicate)
	if !duplicate.Duplicate || duplicate.DocumentID != response.DocumentID {
		t.Fatalf("dedup response = %+v", duplicate)
	}
	var jobs int
	_ = database.QueryRow(`SELECT COUNT(*) FROM jobs WHERE type=?`, media.JobParseDocument).Scan(&jobs)
	if jobs != 1 {
		t.Fatalf("jobs = %d", jobs)
	}
}

func TestUploadRejectsBadExtension(t *testing.T) {
	handler, _ := newUploadRig(t)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	body, contentType := multipartBody(t, "file", "evil.exe", "MZ")
	request := httptest.NewRequest(http.MethodPost, "/api/kbs/kb1/documents", body)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", recorder.Code)
	}
}

func TestUploadRejectsUnknownKnowledgeBase(t *testing.T) {
	handler, _ := newUploadRig(t)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	body, contentType := multipartBody(t, "file", "orphan.md", "content")
	request := httptest.NewRequest(http.MethodPost, "/api/kbs/missing/documents", body)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("code = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestUploadRejectsOversize(t *testing.T) {
	handler, _ := newUploadRig(t)
	handler.MaxBytes = 128
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	body, contentType := multipartBody(t, "file", "large.txt", strings.Repeat("x", 256))
	request := httptest.NewRequest(http.MethodPost, "/api/kbs/kb1/documents", body)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
}
