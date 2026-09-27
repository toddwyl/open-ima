package settings

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"open-ima/internal/infrastructure/config"
	"open-ima/internal/infrastructure/meili"
	"open-ima/internal/infrastructure/sqlite"
	"open-ima/internal/rag"
)

func newTestService(t *testing.T) (*Service, *config.Config) {
	t.Helper()
	database, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/indexes/chunks" {
			_, _ = io.WriteString(w, `{"uid":"chunks"}`)
			return
		}
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"status":"succeeded"}`)
			return
		}
		if r.URL.Path == "/experimental-features" {
			_, _ = io.WriteString(w, `{"vectorStore":true}`)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"taskUid":1}`)
	}))
	t.Cleanup(server.Close)
	client := meili.New(server.URL, "")
	client.PollInterval = time.Millisecond
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Meili.URL, cfg.Meili.Index = server.URL, "chunks"
	return New(database, cfg, client, rag.NewService(rag.Deps{})), cfg
}

func TestUpdateMasksKeyAndPersistsSettings(t *testing.T) {
	service, cfg := newTestService(t)
	updated, err := service.Update(context.Background(), Values{
		LLMProtocol: "anthropic", LLMBaseURL: "https://api.kimi.com/coding/", LLMModel: "kimi-for-coding",
		LLMAPIKey: "secret", EmbedderURL: "http://127.0.0.1:11434/api/embeddings",
		EmbedderModel: "bge-m3", EmbedderDimensions: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.LLMAPIKey != "" || !updated.APIKeyConfigured || cfg.LLM.APIKey != "secret" {
		t.Fatalf("updated=%+v config key=%q", updated, cfg.LLM.APIKey)
	}
	reloaded, _ := config.Load("")
	if err := LoadIntoConfig(context.Background(), service.db, reloaded); err != nil {
		t.Fatal(err)
	}
	if reloaded.LLM.Protocol != "anthropic" || reloaded.LLM.APIKey != "secret" {
		t.Fatalf("reloaded = %+v", reloaded.LLM)
	}
}

func TestUpdateValidatesProtocolAndURLs(t *testing.T) {
	service, _ := newTestService(t)
	_, err := service.Update(context.Background(), Values{LLMProtocol: "bad"})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
