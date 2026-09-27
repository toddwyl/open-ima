package meili

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeMeili struct {
	mu       sync.Mutex
	requests []string
	bodies   []string
	existing map[string]bool
	failTask bool
}

func (f *fakeMeili) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /experimental-features", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		_, _ = io.WriteString(w, `{"vectorStore":true}`)
	})
	mux.HandleFunc("GET /indexes/{uid}", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if f.existing[r.PathValue("uid")] {
			_, _ = fmt.Fprintf(w, `{"uid":%q,"primaryKey":"id"}`, r.PathValue("uid"))
			return
		}
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("POST /indexes", f.acceptTask(1))
	mux.HandleFunc("PATCH /indexes/{uid}/settings", f.acceptTask(2))
	mux.HandleFunc("POST /indexes/{uid}/documents", f.acceptTask(3))
	mux.HandleFunc("POST /indexes/{uid}/documents/delete", f.acceptTask(4))
	mux.HandleFunc("GET /tasks/{uid}", func(w http.ResponseWriter, _ *http.Request) {
		if f.failTask {
			_, _ = io.WriteString(w, `{"status":"failed","error":{"message":"index already exists"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"succeeded"}`)
	})
	return mux
}

func (f *fakeMeili) acceptTask(uid int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"taskUid":%d}`, uid)
	}
}

func (f *fakeMeili) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.bodies = append(f.bodies, string(body))
}

func newFake(t *testing.T) (*Client, *fakeMeili) {
	t.Helper()
	fake := &fakeMeili{existing: map[string]bool{}}
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	client := New(server.URL, "test-key")
	client.PollInterval = time.Millisecond
	client.TaskTimeout = 5 * time.Second
	return client, fake
}

func TestEnsureIndexCreatesAndConfigures(t *testing.T) {
	client, fake := newFake(t)
	if err := client.EnsureIndex(context.Background(), "chunks", EmbedderConfig{URL: "http://ollama/api/embeddings", Model: "bge-m3", Dimensions: 1024}); err != nil {
		t.Fatal(err)
	}
	joined := fmt.Sprint(fake.requests)
	for _, want := range []string{"PATCH /experimental-features", "GET /indexes/chunks", "POST /indexes", "PATCH /indexes/chunks/settings"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing request %s in %v", want, fake.requests)
		}
	}
	var settings map[string]any
	for index, request := range fake.requests {
		if request == "PATCH /indexes/chunks/settings" {
			_ = json.Unmarshal([]byte(fake.bodies[index]), &settings)
		}
	}
	embedder := settings["embedders"].(map[string]any)["default"].(map[string]any)
	if embedder["source"] != "ollama" || embedder["url"] != "http://ollama/api/embeddings" || embedder["model"] != "bge-m3" || embedder["dimensions"].(float64) != 1024 {
		t.Errorf("embedder settings = %v", embedder)
	}
	if fake.bodies[0] != `{"vectorStore":true}` {
		t.Fatalf("experimental features body = %s", fake.bodies[0])
	}
}

func TestEnsureIndexSkipsCreateWhenExists(t *testing.T) {
	client, fake := newFake(t)
	fake.existing["chunks"] = true
	if err := client.EnsureIndex(context.Background(), "chunks", EmbedderConfig{Dimensions: 1024}); err != nil {
		t.Fatal(err)
	}
	for _, request := range fake.requests {
		if request == "POST /indexes" {
			t.Fatal("should not create existing index")
		}
	}
}

func TestAddDocumentsPostsDocsAndWaits(t *testing.T) {
	client, fake := newFake(t)
	docs := []ChunkDoc{{
		ID: "c1", KBID: "kb1", DocumentID: "d1", Title: "t", Content: "hello",
	}}
	if err := client.AddDocuments(context.Background(), "chunks", docs); err != nil {
		t.Fatal(err)
	}
	var posted []map[string]any
	for index, request := range fake.requests {
		if request == "POST /indexes/chunks/documents" {
			_ = json.Unmarshal([]byte(fake.bodies[index]), &posted)
		}
	}
	if len(posted) != 1 || posted[0]["kb_biz_id"] != "kb1" {
		t.Fatalf("posted = %v", posted)
	}
	if _, exists := posted[0]["_vectors"]; exists {
		t.Fatalf("Meilisearch-managed documents must not include _vectors: %v", posted[0])
	}
}

func TestDeleteByFilter(t *testing.T) {
	client, fake := newFake(t)
	if err := client.DeleteByFilter(context.Background(), "chunks", "document_biz_id = 'd1'"); err != nil {
		t.Fatal(err)
	}
	for index, request := range fake.requests {
		if request == "POST /indexes/chunks/documents/delete" && fake.bodies[index] == `{"filter":"document_biz_id = 'd1'"}` {
			return
		}
	}
	t.Fatalf("requests = %v bodies = %v", fake.requests, fake.bodies)
}

func TestFailedTaskReturnsError(t *testing.T) {
	client, fake := newFake(t)
	fake.failTask = true
	err := client.AddDocuments(context.Background(), "chunks", []ChunkDoc{{ID: "x"}})
	if err == nil || !strings.Contains(err.Error(), "index already exists") {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthHeaderSent(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		http.NotFound(w, r)
	}))
	defer server.Close()
	client := New(server.URL, "test-key")
	err := client.EnsureIndex(context.Background(), "x", EmbedderConfig{Dimensions: 8})
	if err == nil {
		t.Fatal("expected create failure")
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("auth = %q", gotAuth)
	}
}

func TestWriteNotFoundReturnsImmediately(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client := New(server.URL, "")
	client.TaskTimeout = time.Second
	start := time.Now()
	err := client.AddDocuments(context.Background(), "missing", []ChunkDoc{{ID: "x"}})
	if err == nil || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("err = %v elapsed = %s", err, time.Since(start))
	}
}

func TestSearchHybridRequestAndResponse(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/indexes/chunks/search" || r.Method != http.MethodPost {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"hits":[{"id":"c1","kb_biz_id":"kb1","document_biz_id":"d1","title":"Doc","content":"plain","_formatted":{"content":"<em>plain</em>"},"_rankingScore":0.9}]}`)
	}))
	defer server.Close()
	client := New(server.URL, "")
	hits, err := client.Search(context.Background(), "chunks", SearchRequest{
		Query: "plain", Filter: "kb_biz_id = 'kb1'", Limit: 8, Hybrid: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Formatted != "<em>plain</em>" || hits[0].Score != 0.9 {
		t.Fatalf("hits = %+v", hits)
	}
	hybrid, ok := body["hybrid"].(map[string]any)
	if !ok || hybrid["semanticRatio"] != 0.5 || body["filter"] != "kb_biz_id = 'kb1'" {
		t.Fatalf("body = %v", body)
	}
	if _, exists := body["vector"]; exists {
		t.Fatalf("Meilisearch-managed search must not include vector: %v", body)
	}
}

func TestSearchTextOmitsVectorAndHybrid(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"hits":[]}`)
	}))
	defer server.Close()
	client := New(server.URL, "")
	if _, err := client.Search(context.Background(), "chunks", SearchRequest{Query: "q"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["vector"]; ok {
		t.Fatalf("text search sent vector: %v", body)
	}
	if _, ok := body["hybrid"]; ok {
		t.Fatalf("text search sent hybrid: %v", body)
	}
}
