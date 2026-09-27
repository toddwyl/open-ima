package db

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
		ChatModels:            []settings.ChatModel{{ModelBizID: "kimi-id", Name: "Kimi", Protocol: "anthropic", BaseURL: "https://api.example.com", Model: "kimi", APIKey: "secret"}},
		DefaultChatModelBizID: "kimi-id", EmbedderURL: "http://127.0.0.1:11434/api/embeddings",
		EmbedderModel: "bge-m3", EmbedderDimensions: 1024,
	}
	if err := repo.Save(ctx, svc.Encode(values)); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	overlaid, err := svc.Overlay(settings.Values{EmbedderDimensions: 512}, stored)
	if err != nil {
		t.Fatal(err)
	}
	if overlaid.ChatModels[0].Protocol != "anthropic" || overlaid.ChatModels[0].APIKey != "secret" || overlaid.EmbedderDimensions != 1024 {
		t.Fatalf("overlaid = %+v", overlaid)
	}

	// 更新单个字段时其余键保持。
	if err := repo.Save(ctx, map[string]string{settings.KeyDefaultChatModelBizID: "other-id"}); err != nil {
		t.Fatal(err)
	}
	stored, _ = repo.Load(ctx)
	if stored[settings.KeyDefaultChatModelBizID] != "other-id" || stored[settings.KeyChatModels] == "" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestSettingsDomainRules(t *testing.T) {
	svc := settings.NewSettingsService()
	v := settings.Values{ChatModels: []settings.ChatModel{{ModelBizID: "m1", Name: " Model ", Protocol: " OpenAI ", BaseURL: "https://a.example.com/", Model: " m "}}, DefaultChatModelBizID: "m1",
		EmbedderURL: "http://b.example.com/", EmbedderModel: "e", EmbedderDimensions: 1}
	svc.Normalize(&v)
	if v.ChatModels[0].Protocol != "openai" || v.ChatModels[0].BaseURL != "https://a.example.com" || v.ChatModels[0].Model != "m" {
		t.Fatalf("normalized = %+v", v)
	}
	if err := svc.Validate(v); err != nil {
		t.Fatal(err)
	}
	if err := svc.Validate(settings.Values{ChatModels: []settings.ChatModel{{ModelBizID: "bad", Name: "bad", Protocol: "bad"}}}); err == nil {
		t.Fatal("expected protocol validation error")
	}
	merged := svc.MergeAPIKeys([]settings.ChatModel{{ModelBizID: "m1", APIKey: "current"}}, []settings.ChatModel{{ModelBizID: "m1"}})
	if merged[0].APIKey != "current" {
		t.Fatalf("keep = %q", merged[0].APIKey)
	}
	public := svc.Public(settings.Values{ChatModels: []settings.ChatModel{{ModelBizID: "m1", APIKey: "secret"}}})
	if public.ChatModels[0].APIKey != "" || !public.ChatModels[0].APIKeyConfigured {
		t.Fatalf("public = %+v", public)
	}
}
