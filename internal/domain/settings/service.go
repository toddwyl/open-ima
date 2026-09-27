package settings

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Service 承载设置的归一化、校验与键值编解码规则。
type SettingsService struct{}

func NewSettingsService() *SettingsService { return &SettingsService{} }

// Normalize 原地规整用户输入:协议小写、URL 去尾斜杠、模型名去空白。
func (s *SettingsService) Normalize(v *Values) {
	v.LLMProtocol = strings.ToLower(strings.TrimSpace(v.LLMProtocol))
	v.LLMBaseURL = strings.TrimRight(strings.TrimSpace(v.LLMBaseURL), "/")
	v.LLMModel = strings.TrimSpace(v.LLMModel)
	v.EmbedderURL = strings.TrimRight(strings.TrimSpace(v.EmbedderURL), "/")
	v.EmbedderModel = strings.TrimSpace(v.EmbedderModel)
}

// Validate 校验归一化后的取值。
func (s *SettingsService) Validate(v Values) error {
	if v.LLMProtocol != "openai" && v.LLMProtocol != "anthropic" {
		return errors.New("llm_protocol must be openai or anthropic")
	}
	for name, raw := range map[string]string{"llm_base_url": v.LLMBaseURL, "embedder_url": v.EmbedderURL} {
		parsed, err := url.ParseRequestURI(raw)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("%s must be an http(s) URL", name)
		}
	}
	if v.LLMModel == "" || v.EmbedderModel == "" {
		return errors.New("model names are required")
	}
	if v.EmbedderDimensions <= 0 || v.EmbedderDimensions > 65536 {
		return errors.New("embedder_dimensions must be between 1 and 65536")
	}
	return nil
}

// MergeAPIKey 计算更新后的 API key:ClearAPIKey 优先,其次新值,否则保留现状。
func (s *SettingsService) MergeAPIKey(current string, next Values) string {
	if next.ClearAPIKey {
		return ""
	}
	if next.LLMAPIKey != "" {
		return next.LLMAPIKey
	}
	return current
}

// Encode 将取值序列化为持久化键值对。
func (s *SettingsService) Encode(v Values) map[string]string {
	return map[string]string{
		KeyLLMProtocol:        v.LLMProtocol,
		KeyLLMBaseURL:         v.LLMBaseURL,
		KeyLLMAPIKey:          v.LLMAPIKey,
		KeyLLMModel:           v.LLMModel,
		KeyEmbedderURL:        v.EmbedderURL,
		KeyEmbedderModel:      v.EmbedderModel,
		KeyEmbedderDimensions: fmt.Sprint(v.EmbedderDimensions),
	}
}

// Overlay 以已持久化的键值对覆盖 base 中对应字段;未持久化的键保持 base 值。
func (s *SettingsService) Overlay(base Values, stored map[string]string) (Values, error) {
	for key, value := range stored {
		switch key {
		case KeyLLMProtocol:
			base.LLMProtocol = value
		case KeyLLMBaseURL:
			base.LLMBaseURL = value
		case KeyLLMAPIKey:
			base.LLMAPIKey = value
		case KeyLLMModel:
			base.LLMModel = value
		case KeyEmbedderURL:
			base.EmbedderURL = value
		case KeyEmbedderModel:
			base.EmbedderModel = value
		case KeyEmbedderDimensions:
			if _, err := fmt.Sscan(value, &base.EmbedderDimensions); err != nil {
				return Values{}, fmt.Errorf("decode %s: %w", key, err)
			}
		}
	}
	return base, nil
}

// Public 返回对外呈现的取值:隐藏 API key,仅暴露是否已配置。
func (s *SettingsService) Public(v Values) Values {
	return Values{
		LLMProtocol: v.LLMProtocol, LLMBaseURL: v.LLMBaseURL, LLMModel: v.LLMModel,
		APIKeyConfigured: v.LLMAPIKey != "", EmbedderURL: v.EmbedderURL,
		EmbedderModel: v.EmbedderModel, EmbedderDimensions: v.EmbedderDimensions,
	}
}
