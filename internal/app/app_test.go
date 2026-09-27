package app

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

	"open-ima/internal/infrastructure/config"
	"open-ima/internal/infrastructure/db"
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
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/search") {
			mocks.mu.Lock()
			documents := append([]map[string]any(nil), mocks.meiliDocs...)
			mocks.mu.Unlock()
			hits := make([]map[string]any, len(documents))
			for index, document := range documents {
				hits[index] = map[string]any{
					"id": document["id"], "kb_biz_id": document["kb_biz_id"], "document_biz_id": document["document_biz_id"],
					"title": document["title"], "content": document["content"],
					"_formatted": map[string]any{"content": "<em>正文内容</em>"}, "_rankingScore": 0.9,
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hits": hits})
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

	chatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if !request.Stream {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"正文内容是什么"}}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"这是回答\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"[1]\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(chatServer.Close)

	cfg := &config.Config{DataDir: filepath.Join(t.TempDir(), "data"), PublicBaseURL: "http://app:8080"}
	cfg.Parser.URL = parserServer.URL
	cfg.Meili.URL = meiliServer.URL
	cfg.Meili.Index = "chunks"
	cfg.Meili.EmbedderURL = "http://ollama:11434/api/embeddings"
	cfg.Meili.EmbedderModel = "bge-m3"
	cfg.Meili.EmbedderDimensions = 1024
	cfg.LLM.BaseURL = chatServer.URL
	cfg.LLM.Model = "test-chat"
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
	if len(mocks.meiliDocs) != 1 || mocks.meiliDocs[0]["kb_biz_id"] != knowledgeBaseID {
		mocks.mu.Unlock()
		t.Fatalf("meili documents = %v", mocks.meiliDocs)
	}
	if _, ok := mocks.meiliDocs[0]["_vectors"]; ok {
		mocks.mu.Unlock()
		t.Fatalf("Meilisearch-managed document contains _vectors: %v", mocks.meiliDocs[0])
	}
	mocks.mu.Unlock()

	recorder, _ = doJSON(t, server.Handler, http.MethodGet,
		"/api/kbs/"+knowledgeBaseID+"/search?q="+"正文"+"&mode=hybrid", nil)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), uploadResponse.DocumentID) {
		t.Fatalf("search: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder, _ = doJSON(t, server.Handler, http.MethodPost, "/api/kbs/"+knowledgeBaseID+"/chat", map[string]string{"query": "正文内容是什么"})
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "event: citations") || !strings.Contains(recorder.Body.String(), "这是回答") {
		t.Fatalf("chat: %d %s", recorder.Code, recorder.Body.String())
	}
	var conversationID string
	for _, block := range strings.Split(recorder.Body.String(), "\n\n") {
		if strings.HasPrefix(block, "event: done") {
			lines := strings.Split(block, "\n")
			var done map[string]string
			_ = json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &done)
			conversationID = done["conversation_id"]
		}
	}
	if conversationID == "" {
		t.Fatal("chat did not return conversation id")
	}
	recorder, _ = doJSON(t, server.Handler, http.MethodGet, "/api/conversations/"+conversationID+"/messages", nil)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "这是回答") {
		t.Fatalf("history: %d %s", recorder.Code, recorder.Body.String())
	}

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
