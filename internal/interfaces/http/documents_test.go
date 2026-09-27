package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"open-ima/internal/application/ingest"
	"open-ima/internal/domain/media"
)

func newUploadMux(services *testServices, maxBytes int64) *http.ServeMux {
	mux := http.NewServeMux()
	(&documentsHandler{ingest: services.ingest, store: services.store, maxBytes: maxBytes}).register(mux)
	return mux
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
	services := newTestServices(t)
	mux := newUploadMux(services, 50<<20)
	body, contentType := multipartBody(t, "file", "笔记.md", "# 标题\n\n正文")
	request := httptest.NewRequest(http.MethodPost, "/api/kbs/kb1/documents", body)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("code = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		DocumentBizID string `json:"document_biz_id"`
		Duplicate     bool   `json:"duplicate"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.DocumentBizID == "" || response.Duplicate {
		t.Fatalf("response = %+v err=%v", response, err)
	}
	var status, fileType string
	_ = services.database.QueryRow(`SELECT status, file_type FROM documents WHERE document_biz_id=?`, response.DocumentBizID).Scan(&status, &fileType)
	if status != document.StatusPending || fileType != "md" {
		t.Fatalf("status=%s type=%s", status, fileType)
	}

	body, contentType = multipartBody(t, "file", "改名.md", "# 标题\n\n正文")
	request = httptest.NewRequest(http.MethodPost, "/api/kbs/kb1/documents", body)
	request.Header.Set("Content-Type", contentType)
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	var duplicate struct {
		DocumentBizID string `json:"document_biz_id"`
		Duplicate     bool   `json:"duplicate"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &duplicate)
	if !duplicate.Duplicate || duplicate.DocumentBizID != response.DocumentBizID {
		t.Fatalf("dedup response = %+v", duplicate)
	}
	var jobs int
	_ = services.database.QueryRow(`SELECT COUNT(*) FROM jobs WHERE type=?`, ingest.JobParseDocument).Scan(&jobs)
	if jobs != 1 {
		t.Fatalf("jobs = %d", jobs)
	}
}

func TestUploadRejectsBadExtension(t *testing.T) {
	mux := newUploadMux(newTestServices(t), 50<<20)
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
	mux := newUploadMux(newTestServices(t), 50<<20)
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
	mux := newUploadMux(newTestServices(t), 128)
	body, contentType := multipartBody(t, "file", "large.txt", strings.Repeat("x", 256))
	request := httptest.NewRequest(http.MethodPost, "/api/kbs/kb1/documents", body)
	request.Header.Set("Content-Type", contentType)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("code = %d body=%s", recorder.Code, recorder.Body.String())
	}
}
