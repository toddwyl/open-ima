package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "./data" || cfg.HTTPAddr != ":8080" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.Worker.Concurrency != 4 {
		t.Errorf("worker concurrency = %d, want 4", cfg.Worker.Concurrency)
	}
	if cfg.LLM.Protocol != "openai" {
		t.Errorf("llm protocol = %q", cfg.LLM.Protocol)
	}
	if cfg.Meili.URL != "http://127.0.0.1:7700" || cfg.Meili.Index != "chunks" {
		t.Errorf("meili defaults: %+v", cfg.Meili)
	}
	if cfg.Meili.EmbedderURL != "http://127.0.0.1:11434/api/embeddings" || cfg.Meili.EmbedderModel != "bge-m3" {
		t.Errorf("meili embedder defaults: %+v", cfg.Meili)
	}
	if cfg.Parser.URL != "http://localhost:8100" {
		t.Errorf("parser url = %q", cfg.Parser.URL)
	}
	if cfg.Meili.EmbedderDimensions != 1024 {
		t.Errorf("dimensions = %d", cfg.Meili.EmbedderDimensions)
	}
	if cfg.PublicBaseURL != "http://localhost:8080" {
		t.Errorf("public base = %q", cfg.PublicBaseURL)
	}
	if cfg.Chunk.Size != 512 || cfg.Chunk.Overlap != 80 {
		t.Errorf("chunk defaults: %+v", cfg.Chunk)
	}
	want := []string{"\n\n", "\n", "。", "?", "!", ";", " "}
	if len(cfg.Chunk.Separators) != len(want) {
		t.Fatalf("chunk separators = %q", cfg.Chunk.Separators)
	}
	for index := range want {
		if cfg.Chunk.Separators[index] != want[index] {
			t.Fatalf("chunk separators = %q, want %q", cfg.Chunk.Separators, want)
		}
	}
}

func TestLoadYAMLAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "llm:\n  base_url: https://api.deepseek.com/v1\n  model: deepseek-chat\ndata_dir: /tmp/ima\nchunk:\n  size: 300\n  overlap: 30\n  separators: [\"\\n\\n\", \"。\"]\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMA_LLM_API_KEY", "sk-test")
	t.Setenv("IMA_LLM_PROTOCOL", "anthropic")
	t.Setenv("IMA_DATA_DIR", "/tmp/ima-env")
	t.Setenv("IMA_WORKER_CONCURRENCY", "7")
	t.Setenv("IMA_CHUNK_SIZE", "700")
	t.Setenv("IMA_CHUNK_OVERLAP", "70")
	t.Setenv("IMA_CHUNK_SEPARATORS", `\n\n,。,\s`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.BaseURL != "https://api.deepseek.com/v1" || cfg.LLM.Model != "deepseek-chat" {
		t.Errorf("yaml not applied: %+v", cfg.LLM)
	}
	if cfg.LLM.APIKey != "sk-test" {
		t.Errorf("env api key not applied")
	}
	if cfg.LLM.Protocol != "anthropic" {
		t.Errorf("llm protocol = %q", cfg.LLM.Protocol)
	}
	if cfg.DataDir != "/tmp/ima-env" {
		t.Errorf("env should beat yaml: %q", cfg.DataDir)
	}
	if cfg.DBPath() != "/tmp/ima-env/open-ima.db" {
		t.Errorf("DBPath = %q", cfg.DBPath())
	}
	if cfg.Worker.Concurrency != 7 {
		t.Errorf("worker concurrency = %d", cfg.Worker.Concurrency)
	}
	if cfg.Chunk.Size != 700 || cfg.Chunk.Overlap != 70 {
		t.Errorf("chunk env override: %+v", cfg.Chunk)
	}
	wantSep := []string{"\n\n", "。", " "}
	if len(cfg.Chunk.Separators) != len(wantSep) {
		t.Fatalf("chunk separators = %q", cfg.Chunk.Separators)
	}
	for index := range wantSep {
		if cfg.Chunk.Separators[index] != wantSep[index] {
			t.Fatalf("chunk separators = %q, want %q", cfg.Chunk.Separators, wantSep)
		}
	}
}

func TestLoadNoopOpenerEnv(t *testing.T) {
	t.Setenv("IMA_OPENER_NOOP", "1")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.NoopOpener {
		t.Error("IMA_OPENER_NOOP=1 should enable noop opener")
	}
}
