package sqlite

import (
	"context"
	"testing"

	"open-ima/internal/domain/settings"
)

func TestSettingsSaveLoadRoundTrip(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	repo := NewSettingsRepository(database)
	svc := settings.NewSettingsService()
	ctx := context.Background()

	values := settings.Values{
		LLMProtocol: "anthropic", LLMBaseURL: "https://api.example.com", LLMModel: "kimi",
		LLMAPIKey: "secret", EmbedderURL: "http://127.0.0.1:11434/api/embeddings",
		EmbedderModel: "bge-m3", EmbedderDimensions: 1024,
	}
	if err := repo.Save(ctx, svc.Encode(values)); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	overlaid, err := svc.Overlay(settings.Values{LLMProtocol: "openai", EmbedderDimensions: 512}, stored)
	if err != nil {
		t.Fatal(err)
	}
	if overlaid.LLMProtocol != "anthropic" || overlaid.LLMAPIKey != "secret" || overlaid.EmbedderDimensions != 1024 {
		t.Fatalf("overlaid = %+v", overlaid)
	}

	// 更新单个字段时其余键保持。
	if err := repo.Save(ctx, map[string]string{settings.KeyLLMModel: "other-model"}); err != nil {
		t.Fatal(err)
	}
	stored, _ = repo.Load(ctx)
	if stored[settings.KeyLLMModel] != "other-model" || stored[settings.KeyLLMProtocol] != "anthropic" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestSettingsDomainRules(t *testing.T) {
	svc := settings.NewSettingsService()
	v := settings.Values{LLMProtocol: " OpenAI ", LLMBaseURL: "https://a.example.com/", LLMModel: " m ",
		EmbedderURL: "http://b.example.com/", EmbedderModel: "e", EmbedderDimensions: 1}
	svc.Normalize(&v)
	if v.LLMProtocol != "openai" || v.LLMBaseURL != "https://a.example.com" || v.LLMModel != "m" {
		t.Fatalf("normalized = %+v", v)
	}
	if err := svc.Validate(v); err != nil {
		t.Fatal(err)
	}
	if err := svc.Validate(settings.Values{LLMProtocol: "bad"}); err == nil {
		t.Fatal("expected protocol validation error")
	}
	if got := svc.MergeAPIKey("current", settings.Values{}); got != "current" {
		t.Fatalf("keep = %q", got)
	}
	if got := svc.MergeAPIKey("current", settings.Values{ClearAPIKey: true}); got != "" {
		t.Fatalf("clear = %q", got)
	}
	if got := svc.MergeAPIKey("current", settings.Values{LLMAPIKey: "new"}); got != "new" {
		t.Fatalf("replace = %q", got)
	}
	public := svc.Public(settings.Values{LLMAPIKey: "secret"})
	if public.LLMAPIKey != "" || !public.APIKeyConfigured {
		t.Fatalf("public = %+v", public)
	}
}
