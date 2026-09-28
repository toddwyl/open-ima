package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Service 承载设置的归一化、校验与键值编解码规则。
type SettingsService struct{}

func NewSettingsService() *SettingsService { return &SettingsService{} }

// Normalize 原地规整用户输入:协议小写、URL 去尾斜杠、模型名去空白。
func (s *SettingsService) Normalize(v *Values) {
	v.DefaultChatModelBizID = strings.TrimSpace(v.DefaultChatModelBizID)
	for index := range v.ChatModels {
		model := &v.ChatModels[index]
		model.ModelBizID = strings.TrimSpace(model.ModelBizID)
		model.Name = strings.TrimSpace(model.Name)
		model.Protocol = strings.ToLower(strings.TrimSpace(model.Protocol))
		model.BaseURL = strings.TrimRight(strings.TrimSpace(model.BaseURL), "/")
		model.Model = strings.TrimSpace(model.Model)
	}
	v.EmbedderURL = strings.TrimRight(strings.TrimSpace(v.EmbedderURL), "/")
	v.EmbedderModel = strings.TrimSpace(v.EmbedderModel)
	if v.ChunkSize <= 0 {
		v.ChunkSize = DefaultChunkSize
	}
	if v.ChunkOverlap < 0 {
		v.ChunkOverlap = DefaultChunkOverlap
	}
	if len(v.ChunkSeparators) == 0 {
		v.ChunkSeparators = DefaultChunkSeparators()
	}
	v.AnySearchAPIKey = strings.TrimSpace(v.AnySearchAPIKey)
	if v.WebSearchMaxResults <= 0 {
		v.WebSearchMaxResults = DefaultWebSearchMaxResults
	}
}

// Validate 校验归一化后的取值。
func (s *SettingsService) Validate(v Values) error {
	if len(v.ChatModels) == 0 {
		return errors.New("at least one chat model is required")
	}
	seen := make(map[string]bool, len(v.ChatModels))
	for _, model := range v.ChatModels {
		if model.ModelBizID == "" || model.Name == "" || model.Model == "" {
			return errors.New("chat model id, name and model are required")
		}
		if seen[model.ModelBizID] {
			return fmt.Errorf("duplicate chat model id %q", model.ModelBizID)
		}
		seen[model.ModelBizID] = true
		if model.Protocol != "openai" && model.Protocol != "anthropic" {
			return fmt.Errorf("chat model %q protocol must be openai or anthropic", model.Name)
		}
		if err := validateHTTPURL("chat model base_url", model.BaseURL); err != nil {
			return err
		}
	}
	if !seen[v.DefaultChatModelBizID] {
		return errors.New("default_chat_model_biz_id must reference a configured model")
	}
	if err := validateHTTPURL("embedder_url", v.EmbedderURL); err != nil {
		return err
	}
	if v.EmbedderModel == "" {
		return errors.New("embedder model is required")
	}
	if v.EmbedderDimensions <= 0 || v.EmbedderDimensions > 65536 {
		return errors.New("embedder_dimensions must be between 1 and 65536")
	}
	if v.ChunkSize <= 0 || v.ChunkSize > 65536 {
		return errors.New("chunk_size must be between 1 and 65536")
	}
	if v.ChunkOverlap < 0 || v.ChunkOverlap >= v.ChunkSize {
		return errors.New("chunk_overlap must be >= 0 and less than chunk_size")
	}
	for _, separator := range v.ChunkSeparators {
		if separator == "" {
			return errors.New("chunk_separators must not contain empty strings")
		}
	}
	if v.WebSearchMaxResults < 1 || v.WebSearchMaxResults > 20 {
		return errors.New("web_search_max_results must be between 1 and 20")
	}
	return nil
}

func validateHTTPURL(name, raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%s must be an http(s) URL", name)
	}
	return nil
}

// MergeAPIKeys 在更新未携带密钥时保留对应模型的现有密钥。
func (s *SettingsService) MergeAPIKeys(current, next []ChatModel) []ChatModel {
	keys := make(map[string]string, len(current))
	for _, model := range current {
		keys[model.ModelBizID] = model.APIKey
	}
	for index := range next {
		if next[index].ClearAPIKey {
			next[index].APIKey = ""
		} else if next[index].APIKey == "" {
			next[index].APIKey = keys[next[index].ModelBizID]
		}
		next[index].ClearAPIKey = false
	}
	return next
}

// MergeAnySearchKey 在更新未携带 AnySearch 密钥时保留现有密钥;Clear 指令优先。
func (s *SettingsService) MergeAnySearchKey(current Values, next *Values) {
	if next.ClearAnySearchAPIKey {
		next.AnySearchAPIKey = ""
	} else if next.AnySearchAPIKey == "" {
		next.AnySearchAPIKey = current.AnySearchAPIKey
	}
	next.ClearAnySearchAPIKey = false
}

// Encode 将取值序列化为持久化键值对。
func (s *SettingsService) Encode(v Values) map[string]string {
	models, _ := json.Marshal(v.ChatModels)
	separators, _ := json.Marshal(v.ChunkSeparators)
	return map[string]string{
		KeyChatModels:            string(models),
		KeyDefaultChatModelBizID: v.DefaultChatModelBizID,
		KeyEmbedderURL:           v.EmbedderURL,
		KeyEmbedderModel:         v.EmbedderModel,
		KeyEmbedderDimensions:    fmt.Sprint(v.EmbedderDimensions),
		KeyChunkSize:             fmt.Sprint(v.ChunkSize),
		KeyChunkOverlap:          fmt.Sprint(v.ChunkOverlap),
		KeyChunkSeparators:       string(separators),
		KeyWebSearchEnabled:      strconv.FormatBool(v.WebSearchEnabled),
		KeyWebSearchMaxResults:   fmt.Sprint(v.WebSearchMaxResults),
		KeyAnySearchAPIKey:       v.AnySearchAPIKey,
	}
}

// Overlay 以已持久化的键值对覆盖 base 中对应字段;未持久化的键保持 base 值。
func (s *SettingsService) Overlay(base Values, stored map[string]string) (Values, error) {
	if raw, ok := stored[KeyChatModels]; ok {
		if err := json.Unmarshal([]byte(raw), &base.ChatModels); err != nil {
			return Values{}, fmt.Errorf("decode %s: %w", KeyChatModels, err)
		}
	} else if legacyModel := stored[KeyLLMModel]; legacyModel != "" {
		base.ChatModels = []ChatModel{{ModelBizID: DefaultModelBizID, Name: legacyModel,
			Protocol: stored[KeyLLMProtocol], BaseURL: stored[KeyLLMBaseURL], Model: legacyModel, APIKey: stored[KeyLLMAPIKey]}}
	}
	if value := stored[KeyDefaultChatModelBizID]; value != "" {
		base.DefaultChatModelBizID = value
	}
	for key, value := range stored {
		switch key {
		case KeyEmbedderURL:
			base.EmbedderURL = value
		case KeyEmbedderModel:
			base.EmbedderModel = value
		case KeyEmbedderDimensions:
			if _, err := fmt.Sscan(value, &base.EmbedderDimensions); err != nil {
				return Values{}, fmt.Errorf("decode %s: %w", key, err)
			}
		case KeyChunkSize:
			if _, err := fmt.Sscan(value, &base.ChunkSize); err != nil {
				return Values{}, fmt.Errorf("decode %s: %w", key, err)
			}
		case KeyChunkOverlap:
			if _, err := fmt.Sscan(value, &base.ChunkOverlap); err != nil {
				return Values{}, fmt.Errorf("decode %s: %w", key, err)
			}
		case KeyChunkSeparators:
			if err := json.Unmarshal([]byte(value), &base.ChunkSeparators); err != nil {
				return Values{}, fmt.Errorf("decode %s: %w", key, err)
			}
		case KeyWebSearchEnabled:
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return Values{}, fmt.Errorf("decode %s: %w", key, err)
			}
			base.WebSearchEnabled = parsed
		case KeyAnySearchAPIKey:
			base.AnySearchAPIKey = value
		case KeyWebSearchMaxResults:
			if _, err := fmt.Sscan(value, &base.WebSearchMaxResults); err != nil {
				return Values{}, fmt.Errorf("decode %s: %w", key, err)
			}
		}
	}
	return base, nil
}

// Public 返回对外呈现的取值:隐藏 API key,仅暴露是否已配置。
func (s *SettingsService) Public(v Values) Values {
	public := v
	public.ChatModels = make([]ChatModel, len(v.ChatModels))
	for index, model := range v.ChatModels {
		model.APIKeyConfigured = model.APIKey != ""
		model.APIKey = ""
		model.ClearAPIKey = false
		public.ChatModels[index] = model
	}
	public.AnySearchAPIKeyConfigured = v.AnySearchAPIKey != ""
	public.AnySearchAPIKey = ""
	public.ClearAnySearchAPIKey = false
	return public
}
