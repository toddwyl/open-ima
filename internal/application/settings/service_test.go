package settings

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"open-ima/internal/application/port"
	"open-ima/internal/domain/media"
	settingsdom "open-ima/internal/domain/settings"
	"open-ima/internal/infrastructure/db"
	"open-ima/internal/infrastructure/llm"
	"open-ima/internal/infrastructure/meili"
)

type fakeReconfigurer struct {
	models            map[string]port.ChatModel
	defaultModelBizID string
	chunker           *media.Chunker
}

func (f *fakeReconfigurer) SetModels(models map[string]port.ChatModel, defaultModelBizID string) {
	f.models, f.defaultModelBizID = models, defaultModelBizID
}

func (f *fakeReconfigurer) SetChunker(chunker *media.Chunker) { f.chunker = chunker }

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
		repo, settingsdom.NewSettingsService(), client, "chunks", reconfigure, reconfigure,
		func(protocol, baseURL, apiKey, model string) port.ChatModel {
			return llm.NewChatClientWithProtocol(protocol, baseURL, apiKey, model)
		},
		settingsdom.Values{ChatModels: []settingsdom.ChatModel{{ModelBizID: "kimi-id", APIKey: "old"}}, DefaultChatModelBizID: "kimi-id", EmbedderDimensions: 1024},
	)
	return service, repo, reconfigure
}

func TestUpdateMasksKeyAndPersistsSettings(t *testing.T) {
	service, repo, reconfigure := newTestService(t)
	updated, err := service.Update(context.Background(), settingsdom.Values{
		ChatModels:            []settingsdom.ChatModel{{ModelBizID: "kimi-id", Name: "Kimi", Protocol: "anthropic", BaseURL: "https://api.kimi.com/coding/", Model: "kimi-for-coding", APIKey: "secret"}},
		DefaultChatModelBizID: "kimi-id", EmbedderURL: "http://127.0.0.1:11434/api/embeddings",
		EmbedderModel: "bge-m3", EmbedderDimensions: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ChatModels[0].APIKey != "" || !updated.ChatModels[0].APIKeyConfigured {
		t.Fatalf("updated = %+v", updated)
	}
	if reconfigure.models["kimi-id"] == nil || reconfigure.defaultModelBizID != "kimi-id" {
		t.Fatal("chat models should be hot-swapped")
	}
	stored, err := repo.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	overlaid, err := settingsdom.NewSettingsService().Overlay(settingsdom.Values{}, stored)
	if err != nil {
		t.Fatal(err)
	}
	if overlaid.ChatModels[0].Protocol != "anthropic" || overlaid.ChatModels[0].APIKey != "secret" || overlaid.ChatModels[0].BaseURL != "https://api.kimi.com/coding" {
		t.Fatalf("overlaid = %+v", overlaid)
	}
	if got := service.Get(); got.ChatModels[0].APIKey != "" || !got.ChatModels[0].APIKeyConfigured || got.ChatModels[0].Model != "kimi-for-coding" {
		t.Fatalf("get = %+v", got)
	}
}

func TestUpdateValidatesProtocolAndURLs(t *testing.T) {
	service, _, _ := newTestService(t)
	if _, err := service.Update(context.Background(), settingsdom.Values{ChatModels: []settingsdom.ChatModel{{ModelBizID: "bad", Name: "bad", Protocol: "bad"}}}); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestUpdateHotSwapsChunker(t *testing.T) {
	service, repo, reconfigure := newTestService(t)
	updated, err := service.Update(context.Background(), settingsdom.Values{
		ChatModels:            []settingsdom.ChatModel{{ModelBizID: "kimi-id", Name: "Kimi", Protocol: "anthropic", BaseURL: "https://api.kimi.com/coding", Model: "kimi-for-coding"}},
		DefaultChatModelBizID: "kimi-id", EmbedderURL: "http://127.0.0.1:11434/api/embeddings",
		EmbedderModel: "bge-m3", EmbedderDimensions: 1024,
		ChunkSize: 300, ChunkOverlap: 30, ChunkSeparators: []string{"\n\n", "；"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ChunkSize != 300 || updated.ChunkOverlap != 30 {
		t.Fatalf("updated chunk = %+v", updated)
	}
	if reconfigure.chunker == nil {
		t.Fatal("chunker should be hot-swapped")
	}
	// 全角分号不在默认分隔符中,能验证自定义分隔符确实生效:
	// 超长块按 "；" 切分,首块以 "；" 结尾,而非默认的硬切。
	long := strings.Repeat("甲", 200) + "；" + strings.Repeat("乙", 200)
	chunks := reconfigure.chunker.Chunk([]media.Block{{Type: "paragraph", Text: long}})
	if len(chunks) != 2 || !strings.HasSuffix(chunks[0].Content, "；") {
		t.Fatalf("custom separators not applied, got %d chunks", len(chunks))
	}
	stored, err := repo.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	overlaid, err := settingsdom.NewSettingsService().Overlay(settingsdom.Values{}, stored)
	if err != nil {
		t.Fatal(err)
	}
	if overlaid.ChunkSize != 300 || overlaid.ChunkOverlap != 30 || len(overlaid.ChunkSeparators) != 2 {
		t.Fatalf("persisted chunk = %+v", overlaid)
	}
}

func TestUpdateClearsAPIKey(t *testing.T) {
	service, repo, _ := newTestService(t)
	ctx := context.Background()
	if _, err := service.Update(ctx, settingsdom.Values{
		ChatModels:            []settingsdom.ChatModel{{ModelBizID: "kimi-id", Name: "Kimi", Protocol: "openai", BaseURL: "https://api.example.com", Model: "m", APIKey: "secret"}},
		DefaultChatModelBizID: "kimi-id", EmbedderURL: "http://127.0.0.1:11434/api/embeddings",
		EmbedderModel: "bge-m3", EmbedderDimensions: 1024,
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := service.Update(ctx, settingsdom.Values{
		ChatModels:            []settingsdom.ChatModel{{ModelBizID: "kimi-id", Name: "Kimi", Protocol: "openai", BaseURL: "https://api.example.com", Model: "m", ClearAPIKey: true}},
		DefaultChatModelBizID: "kimi-id", EmbedderURL: "http://127.0.0.1:11434/api/embeddings",
		EmbedderModel: "bge-m3", EmbedderDimensions: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ChatModels[0].APIKeyConfigured {
		t.Fatalf("updated = %+v", updated)
	}
	stored, _ := repo.Load(ctx)
	var persisted settingsdom.Values
	persisted, _ = settingsdom.NewSettingsService().Overlay(persisted, stored)
	if persisted.ChatModels[0].APIKey != "" {
		t.Fatalf("stored key = %q", persisted.ChatModels[0].APIKey)
	}
}
