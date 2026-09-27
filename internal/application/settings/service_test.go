package settings

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"open-ima/internal/application/port"
	settingsdom "open-ima/internal/domain/settings"
	"open-ima/internal/infrastructure/db"
	"open-ima/internal/infrastructure/llm"
	"open-ima/internal/infrastructure/meili"
)

type fakeReconfigurer struct{ model port.ChatModel }

func (f *fakeReconfigurer) SetModel(model port.ChatModel) { f.model = model }

func newTestService(t *testing.T) (*Service, *db.SettingsRepository, *fakeReconfigurer) {
	t.Helper()
	database, err := db.Open(":memory:")
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
	repo := db.NewSettingsRepository(database)
	reconfigure := &fakeReconfigurer{}
	service := NewService(
		repo, settingsdom.NewSettingsService(), client, "chunks", reconfigure,
		func(protocol, baseURL, apiKey, model string) port.ChatModel {
			return llm.NewChatClientWithProtocol(protocol, baseURL, apiKey, model)
		},
		settingsdom.Values{LLMProtocol: "openai", EmbedderDimensions: 1024},
	)
	return service, repo, reconfigure
}

func TestUpdateMasksKeyAndPersistsSettings(t *testing.T) {
	service, repo, reconfigure := newTestService(t)
	updated, err := service.Update(context.Background(), settingsdom.Values{
		LLMProtocol: "anthropic", LLMBaseURL: "https://api.kimi.com/coding/", LLMModel: "kimi-for-coding",
		LLMAPIKey: "secret", EmbedderURL: "http://127.0.0.1:11434/api/embeddings",
		EmbedderModel: "bge-m3", EmbedderDimensions: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.LLMAPIKey != "" || !updated.APIKeyConfigured {
		t.Fatalf("updated = %+v", updated)
	}
	if reconfigure.model == nil {
		t.Fatal("chat model should be hot-swapped")
	}
	stored, err := repo.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	overlaid, err := settingsdom.NewSettingsService().Overlay(settingsdom.Values{}, stored)
	if err != nil {
		t.Fatal(err)
	}
	if overlaid.LLMProtocol != "anthropic" || overlaid.LLMAPIKey != "secret" || overlaid.LLMBaseURL != "https://api.kimi.com/coding" {
		t.Fatalf("overlaid = %+v", overlaid)
	}
	if got := service.Get(); got.LLMAPIKey != "" || !got.APIKeyConfigured || got.LLMModel != "kimi-for-coding" {
		t.Fatalf("get = %+v", got)
	}
}

func TestUpdateValidatesProtocolAndURLs(t *testing.T) {
	service, _, _ := newTestService(t)
	if _, err := service.Update(context.Background(), settingsdom.Values{LLMProtocol: "bad"}); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestUpdateClearsAPIKey(t *testing.T) {
	service, repo, _ := newTestService(t)
	ctx := context.Background()
	if _, err := service.Update(ctx, settingsdom.Values{
		LLMProtocol: "openai", LLMBaseURL: "https://api.example.com", LLMModel: "m",
		LLMAPIKey: "secret", EmbedderURL: "http://127.0.0.1:11434/api/embeddings",
		EmbedderModel: "bge-m3", EmbedderDimensions: 1024,
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := service.Update(ctx, settingsdom.Values{
		LLMProtocol: "openai", LLMBaseURL: "https://api.example.com", LLMModel: "m",
		ClearAPIKey: true, EmbedderURL: "http://127.0.0.1:11434/api/embeddings",
		EmbedderModel: "bge-m3", EmbedderDimensions: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.APIKeyConfigured {
		t.Fatalf("updated = %+v", updated)
	}
	stored, _ := repo.Load(ctx)
	if stored[settingsdom.KeyLLMAPIKey] != "" {
		t.Fatalf("stored key = %q", stored[settingsdom.KeyLLMAPIKey])
	}
}
