package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"open-ima/internal/config"
	"open-ima/internal/db"
)

type externalMocks struct {
	mu        sync.Mutex
	meiliDocs []map[string]any
}

func newExternalMocks(t *testing.T) (*externalMocks, *config.Config) {
	t.Helper()
	mocks := &externalMocks{}
	parserServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"title":"集成测试文档","blocks":[{"type":"heading","text":"第一章","level":1},{"type":"paragraph","text":"这是正文内容,用于验证端到端入库链路。"}]}`)
	}))
	t.Cleanup(parserServer.Close)

	meiliServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") {
			_, _ = io.WriteString(w, `{"status":"succeeded"}`)
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/indexes/") {
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/documents") {
			var documents []map[string]any
			_ = json.NewDecoder(r.Body).Decode(&documents)
			mocks.mu.Lock()
			mocks.meiliDocs = append(mocks.meiliDocs, documents...)
			mocks.mu.Unlock()
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"taskUid":1}`)
	}))
	t.Cleanup(meiliServer.Close)

	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		data := make([]map[string]any, len(request.Input))
		for index := range request.Input {
			data[index] = map[string]any{"index": index, "embedding": []float64{0.1, 0.2, 0.3}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(embeddingServer.Close)

	cfg := &config.Config{DataDir: filepath.Join(t.TempDir(), "data"), PublicBaseURL: "http://app:8080"}
	cfg.Parser.URL = parserServer.URL
	cfg.Meili.URL = meiliServer.URL
	cfg.Meili.Index = "chunks"
	cfg.Embedding.BaseURL = embeddingServer.URL
	cfg.Embedding.Model = "test"
	cfg.Embedding.Dimensions = 3
	cfg.Worker.Concurrency = 1
	return mocks, cfg
}

func doJSON(t *testing.T, handler http.Handler, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var response map[string]any
	if recorder.Body.Len() > 0 {
		_ = json.Unmarshal(recorder.Body.Bytes(), &response)
	}
	return recorder, response
}

func TestEndToEndIngestion(t *testing.T) {
	mocks, cfg := newExternalMocks(t)
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server, err := New(cfg, database)
	if err != nil {
		t.Fatal(err)
	}
	server.Worker.PollInterval = time.Millisecond

	recorder, knowledgeBase := doJSON(t, server.Handler, http.MethodPost, "/api/kbs", map[string]string{"name": "e2e库"})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create knowledge base: %d %s", recorder.Code, recorder.Body.String())
	}
	knowledgeBaseID := knowledgeBase["id"].(string)

	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	file, _ := writer.CreateFormFile("file", "测试.md")
	_, _ = file.Write([]byte("# 标题\n\n正文"))
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/kbs/"+knowledgeBaseID+"/documents", &buffer)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder = httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("upload: %d %s", recorder.Code, recorder.Body.String())
	}
	var uploadResponse struct {
		DocumentID string `json:"document_id"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &uploadResponse)

	deadline := time.Now().Add(5 * time.Second)
	var documents []map[string]any
	for {
		server.Worker.RunOnce(context.Background())
		recorder, _ = doJSON(t, server.Handler, http.MethodGet, "/api/kbs/"+knowledgeBaseID+"/documents", nil)
		_ = json.Unmarshal(recorder.Body.Bytes(), &documents)
		if len(documents) == 1 && documents[0]["status"] == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("document never became ready: %v", documents)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if documents[0]["id"] != uploadResponse.DocumentID || documents[0]["chunk_count"].(float64) != 1 {
		t.Fatalf("document = %v", documents[0])
	}
	mocks.mu.Lock()
	if len(mocks.meiliDocs) != 1 || mocks.meiliDocs[0]["kb_id"] != knowledgeBaseID {
		mocks.mu.Unlock()
		t.Fatalf("meili documents = %v", mocks.meiliDocs)
	}
	if _, ok := mocks.meiliDocs[0]["_vectors"].(map[string]any)["default"]; !ok {
		mocks.mu.Unlock()
		t.Fatalf("missing vector: %v", mocks.meiliDocs[0])
	}
	mocks.mu.Unlock()

	recorder, _ = doJSON(t, server.Handler, http.MethodDelete, "/api/documents/"+uploadResponse.DocumentID, nil)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", recorder.Code, recorder.Body.String())
	}
	server.Worker.RunOnce(context.Background())
	recorder, _ = doJSON(t, server.Handler, http.MethodGet, "/api/kbs/"+knowledgeBaseID+"/documents", nil)
	if recorder.Body.String() != "null\n" && recorder.Body.String() != "[]\n" {
		t.Fatalf("documents after delete: %s", recorder.Body.String())
	}
}

func TestHealth(t *testing.T) {
	_, cfg := newExternalMocks(t)
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server, err := New(cfg, database)
	if err != nil {
		t.Fatal(err)
	}
	recorder, response := doJSON(t, server.Handler, http.MethodGet, "/health", nil)
	if recorder.Code != http.StatusOK || response["status"] != "ok" {
		t.Fatalf("health: %d %v", recorder.Code, response)
	}
}
