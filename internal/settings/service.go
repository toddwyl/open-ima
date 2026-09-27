package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"open-ima/internal/infrastructure/config"
	"open-ima/internal/infrastructure/llm"
	"open-ima/internal/infrastructure/meili"
	"open-ima/internal/rag"
)

const (
	keyLLMProtocol        = "llm.protocol"
	keyLLMBaseURL         = "llm.base_url"
	keyLLMAPIKey          = "llm.api_key"
	keyLLMModel           = "llm.model"
	keyEmbedderURL        = "meili.embedder_url"
	keyEmbedderModel      = "meili.embedder_model"
	keyEmbedderDimensions = "meili.embedder_dimensions"
)

type Values struct {
	LLMProtocol        string `json:"llm_protocol"`
	LLMBaseURL         string `json:"llm_base_url"`
	LLMModel           string `json:"llm_model"`
	LLMAPIKey          string `json:"llm_api_key,omitempty"`
	APIKeyConfigured   bool   `json:"api_key_configured"`
	ClearAPIKey        bool   `json:"clear_api_key,omitempty"`
	EmbedderURL        string `json:"embedder_url"`
	EmbedderModel      string `json:"embedder_model"`
	EmbedderDimensions int    `json:"embedder_dimensions"`
}

type Service struct {
	db       *sql.DB
	cfg      *config.Config
	meili    *meili.Client
	rag      *rag.Service
	indexUID string
}

func LoadIntoConfig(ctx context.Context, db *sql.DB, cfg *config.Config) error {
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM app_settings`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		switch key {
		case keyLLMProtocol:
			cfg.LLM.Protocol = value
		case keyLLMBaseURL:
			cfg.LLM.BaseURL = value
		case keyLLMAPIKey:
			cfg.LLM.APIKey = value
		case keyLLMModel:
			cfg.LLM.Model = value
		case keyEmbedderURL:
			cfg.Meili.EmbedderURL = value
		case keyEmbedderModel:
			cfg.Meili.EmbedderModel = value
		case keyEmbedderDimensions:
			if _, err := fmt.Sscan(value, &cfg.Meili.EmbedderDimensions); err != nil {
				return fmt.Errorf("decode %s: %w", key, err)
			}
		}
	}
	return rows.Err()
}

func New(db *sql.DB, cfg *config.Config, meiliClient *meili.Client, ragService *rag.Service) *Service {
	return &Service{db: db, cfg: cfg, meili: meiliClient, rag: ragService, indexUID: cfg.Meili.Index}
}

func (s *Service) Get() Values {
	return Values{
		LLMProtocol: s.cfg.LLM.Protocol, LLMBaseURL: s.cfg.LLM.BaseURL, LLMModel: s.cfg.LLM.Model,
		APIKeyConfigured: s.cfg.LLM.APIKey != "", EmbedderURL: s.cfg.Meili.EmbedderURL,
		EmbedderModel: s.cfg.Meili.EmbedderModel, EmbedderDimensions: s.cfg.Meili.EmbedderDimensions,
	}
}

func (s *Service) Update(ctx context.Context, next Values) (Values, error) {
	next.LLMProtocol = strings.ToLower(strings.TrimSpace(next.LLMProtocol))
	next.LLMBaseURL = strings.TrimRight(strings.TrimSpace(next.LLMBaseURL), "/")
	next.LLMModel = strings.TrimSpace(next.LLMModel)
	next.EmbedderURL = strings.TrimRight(strings.TrimSpace(next.EmbedderURL), "/")
	next.EmbedderModel = strings.TrimSpace(next.EmbedderModel)
	if err := validate(next); err != nil {
		return Values{}, err
	}
	apiKey := s.cfg.LLM.APIKey
	if next.ClearAPIKey {
		apiKey = ""
	} else if next.LLMAPIKey != "" {
		apiKey = next.LLMAPIKey
	}
	if err := s.meili.EnsureIndex(ctx, s.indexUID, meili.EmbedderConfig{
		URL: next.EmbedderURL, Model: next.EmbedderModel, Dimensions: next.EmbedderDimensions,
	}); err != nil {
		return Values{}, fmt.Errorf("apply embedder settings: %w", err)
	}
	values := map[string]string{
		keyLLMProtocol: next.LLMProtocol, keyLLMBaseURL: next.LLMBaseURL, keyLLMAPIKey: apiKey,
		keyLLMModel: next.LLMModel, keyEmbedderURL: next.EmbedderURL, keyEmbedderModel: next.EmbedderModel,
		keyEmbedderDimensions: fmt.Sprint(next.EmbedderDimensions),
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Values{}, err
	}
	defer tx.Rollback()
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, `INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`, key, value); err != nil {
			return Values{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Values{}, err
	}
	s.cfg.LLM.Protocol, s.cfg.LLM.BaseURL, s.cfg.LLM.APIKey, s.cfg.LLM.Model = next.LLMProtocol, next.LLMBaseURL, apiKey, next.LLMModel
	s.cfg.Meili.EmbedderURL, s.cfg.Meili.EmbedderModel, s.cfg.Meili.EmbedderDimensions = next.EmbedderURL, next.EmbedderModel, next.EmbedderDimensions
	s.rag.SetChatClient(llm.NewChatClientWithProtocol(next.LLMProtocol, next.LLMBaseURL, apiKey, next.LLMModel))
	return s.Get(), nil
}

func validate(values Values) error {
	if values.LLMProtocol != "openai" && values.LLMProtocol != "anthropic" {
		return errors.New("llm_protocol must be openai or anthropic")
	}
	for name, raw := range map[string]string{"llm_base_url": values.LLMBaseURL, "embedder_url": values.EmbedderURL} {
		parsed, err := url.ParseRequestURI(raw)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("%s must be an http(s) URL", name)
		}
	}
	if values.LLMModel == "" || values.EmbedderModel == "" {
		return errors.New("model names are required")
	}
	if values.EmbedderDimensions <= 0 || values.EmbedderDimensions > 65536 {
		return errors.New("embedder_dimensions must be between 1 and 65536")
	}
	return nil
}
