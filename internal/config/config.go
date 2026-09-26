// Package config 加载 config.yaml 并应用 IMA_ 前缀环境变量覆盖。
package config

import (
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

type LLMConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
}

type EmbeddingConfig struct {
	BaseURL    string `yaml:"base_url"`
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	Dimensions int    `yaml:"dimensions"`
}

type ParserConfig struct {
	URL string `yaml:"url"`
}

type MeiliConfig struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"api_key"`
	Index  string `yaml:"index"`
}

type WorkerConfig struct {
	Concurrency int `yaml:"concurrency"`
}

type Config struct {
	LLM           LLMConfig       `yaml:"llm"`
	Embedding     EmbeddingConfig `yaml:"embedding"`
	Parser        ParserConfig    `yaml:"parser"`
	Meili         MeiliConfig     `yaml:"meili"`
	DataDir       string          `yaml:"data_dir"`
	Worker        WorkerConfig    `yaml:"worker"`
	HTTPAddr      string          `yaml:"http_addr"`
	PublicBaseURL string          `yaml:"public_base_url"`
}

func defaults() *Config {
	cfg := &Config{
		DataDir:       "./data",
		HTTPAddr:      ":8080",
		PublicBaseURL: "http://localhost:8080",
	}
	cfg.Worker.Concurrency = 4
	cfg.Meili.URL = "http://localhost:7700"
	cfg.Meili.Index = "chunks"
	cfg.Parser.URL = "http://localhost:8100"
	cfg.Embedding.Dimensions = 1024
	return cfg
}

func Load(path string) (*Config, error) {
	cfg := defaults()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}
	applyEnv(cfg)
	return cfg, nil
}

func applyEnv(cfg *Config) {
	setStr := func(dst *string, keys ...string) {
		for _, k := range keys {
			if v := os.Getenv(k); v != "" {
				*dst = v
			}
		}
	}
	setStr(&cfg.LLM.BaseURL, "IMA_LLM_BASE_URL")
	setStr(&cfg.LLM.APIKey, "IMA_LLM_API_KEY")
	setStr(&cfg.LLM.Model, "IMA_LLM_MODEL")
	setStr(&cfg.Embedding.BaseURL, "IMA_EMBEDDING_BASE_URL")
	setStr(&cfg.Embedding.APIKey, "IMA_EMBEDDING_API_KEY")
	setStr(&cfg.Embedding.Model, "IMA_EMBEDDING_MODEL")
	if v := os.Getenv("IMA_EMBEDDING_DIMENSIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Embedding.Dimensions = n
		}
	}
	setStr(&cfg.Parser.URL, "IMA_PARSER_URL")
	setStr(&cfg.Meili.URL, "IMA_MEILI_URL")
	setStr(&cfg.Meili.APIKey, "IMA_MEILI_API_KEY")
	setStr(&cfg.Meili.Index, "IMA_MEILI_INDEX")
	setStr(&cfg.DataDir, "IMA_DATA_DIR")
	setStr(&cfg.HTTPAddr, "IMA_HTTP_ADDR")
	setStr(&cfg.PublicBaseURL, "IMA_PUBLIC_BASE_URL")
}

// DBPath 返回 SQLite 数据库文件路径。
func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "open-ima.db")
}
