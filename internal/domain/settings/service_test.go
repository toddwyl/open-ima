package settings

import "testing"

func validValues() Values {
	return Values{
		ChatModels:            []ChatModel{{ModelBizID: "m1", Name: "Kimi", Protocol: "openai", BaseURL: "https://api.example.com", Model: "kimi"}},
		DefaultChatModelBizID: "m1",
		EmbedderURL:           "http://127.0.0.1:11434/api/embeddings",
		EmbedderModel:         "bge-m3",
		EmbedderDimensions:    1024,
		ChunkSize:             512,
		ChunkOverlap:          80,
		ChunkSeparators:       []string{"\n\n", "\n", "。", "?", "!", ";", " "},
	}
}

func TestNormalizeFillsChunkDefaults(t *testing.T) {
	service := NewSettingsService()
	values := validValues()
	values.ChunkSize, values.ChunkOverlap, values.ChunkSeparators = 0, -1, nil
	service.Normalize(&values)
	if values.ChunkSize != DefaultChunkSize || values.ChunkOverlap != DefaultChunkOverlap {
		t.Fatalf("chunk defaults not filled: %+v", values)
	}
	if len(values.ChunkSeparators) != len(DefaultChunkSeparators()) {
		t.Fatalf("chunk separators not filled: %q", values.ChunkSeparators)
	}
}

func TestValidateChunkSettings(t *testing.T) {
	service := NewSettingsService()
	cases := []struct {
		name   string
		mutate func(*Values)
	}{
		{"zero size", func(v *Values) { v.ChunkSize = 0 }},
		{"negative overlap", func(v *Values) { v.ChunkOverlap = -1 }},
		{"overlap >= size", func(v *Values) { v.ChunkOverlap = v.ChunkSize }},
		{"empty separator", func(v *Values) { v.ChunkSeparators = []string{"\n\n", ""} }},
	}
	for _, testCase := range cases {
		values := validValues()
		testCase.mutate(&values)
		if err := service.Validate(values); err == nil {
			t.Fatalf("%s: expected validation error", testCase.name)
		}
	}
}

func TestChunkSettingsEncodeOverlayRoundtrip(t *testing.T) {
	service := NewSettingsService()
	values := validValues()
	values.ChunkSize, values.ChunkOverlap = 300, 30
	values.ChunkSeparators = []string{"\n\n", "。"}

	encoded := service.Encode(values)
	if encoded[KeyChunkSize] != "300" || encoded[KeyChunkOverlap] != "30" {
		t.Fatalf("encoded chunk keys: %v", encoded)
	}

	base := validValues()
	overlaid, err := service.Overlay(base, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if overlaid.ChunkSize != 300 || overlaid.ChunkOverlap != 30 {
		t.Fatalf("overlaid chunk: %+v", overlaid)
	}
	if len(overlaid.ChunkSeparators) != 2 || overlaid.ChunkSeparators[1] != "。" {
		t.Fatalf("overlaid separators: %q", overlaid.ChunkSeparators)
	}

	// 未持久化的键保持 base 值。
	overlaid, err = service.Overlay(validValues(), map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if overlaid.ChunkSize != 512 || overlaid.ChunkOverlap != 80 {
		t.Fatalf("base chunk changed without stored keys: %+v", overlaid)
	}
}
