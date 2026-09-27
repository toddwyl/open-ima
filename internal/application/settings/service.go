// Package settings 编排应用设置用例:读取、校验、应用并热切换聊天模型。
package settings

import (
	"context"
	"fmt"
	"sync"

	"open-ima/internal/application/port"
	settingsdom "open-ima/internal/domain/settings"
)

// ChatReconfigurer 由 chat 用例实现,用于热切换聊天模型。
type ChatReconfigurer interface {
	SetModel(model port.ChatModel)
}

// ChatModelFactory 按协议构造聊天模型客户端,由装配根注入。
type ChatModelFactory func(protocol, baseURL, apiKey, model string) port.ChatModel

// Service 是应用设置用例,持有当前生效的设置。
type Service struct {
	repo        settingsdom.SettingsRepository
	domain      *settingsdom.SettingsService
	admin       port.SearchAdmin
	indexUID    string
	reconfigure ChatReconfigurer
	newModel    ChatModelFactory

	mu      sync.RWMutex
	current settingsdom.Values
}

func NewService(
	repo settingsdom.SettingsRepository, domain *settingsdom.SettingsService, admin port.SearchAdmin,
	indexUID string, reconfigure ChatReconfigurer, newModel ChatModelFactory,
	initial settingsdom.Values,
) *Service {
	return &Service{
		repo: repo, domain: domain, admin: admin, indexUID: indexUID,
		reconfigure: reconfigure, newModel: newModel, current: initial,
	}
}

// Get 返回对外呈现的设置(隐藏 API key)。
func (s *Service) Get() settingsdom.Values {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.domain.Public(s.current)
}

// Update 校验并应用新设置:先更新检索引擎 embedder,再持久化,最后热切换聊天模型。
func (s *Service) Update(ctx context.Context, next settingsdom.Values) (settingsdom.Values, error) {
	s.domain.Normalize(&next)
	if err := s.domain.Validate(next); err != nil {
		return settingsdom.Values{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	apiKey := s.domain.MergeAPIKey(s.current.LLMAPIKey, next)
	if err := s.admin.EnsureIndex(ctx, s.indexUID, port.EmbedderConfig{
		URL: next.EmbedderURL, Model: next.EmbedderModel, Dimensions: next.EmbedderDimensions,
	}); err != nil {
		return settingsdom.Values{}, fmt.Errorf("apply embedder settings: %w", err)
	}
	next.LLMAPIKey = apiKey
	if err := s.repo.Save(ctx, s.domain.Encode(next)); err != nil {
		return settingsdom.Values{}, err
	}
	s.current = next
	s.reconfigure.SetModel(s.newModel(next.LLMProtocol, next.LLMBaseURL, apiKey, next.LLMModel))
	return s.domain.Public(next), nil
}
