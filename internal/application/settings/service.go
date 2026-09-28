// Package settings 编排应用设置用例:读取、校验、应用并热切换聊天模型。
package settings

import (
	"context"
	"fmt"
	"sync"

	"open-ima/internal/application/port"
	"open-ima/internal/domain/media"
	settingsdom "open-ima/internal/domain/settings"
)

// ChatReconfigurer 由 chat 用例实现,用于热切换聊天模型。
type ChatReconfigurer interface {
	SetModels(models map[string]port.ChatModel, defaultModelBizID string)
}

// ChunkReconfigurer 由 ingest 用例实现,用于热切换文档分块器。
type ChunkReconfigurer interface {
	SetChunker(chunker *media.Chunker)
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
	rechunk     ChunkReconfigurer
	newModel    ChatModelFactory

	mu      sync.RWMutex
	current settingsdom.Values
}

func NewService(
	repo settingsdom.SettingsRepository, domain *settingsdom.SettingsService, admin port.SearchAdmin,
	indexUID string, reconfigure ChatReconfigurer, rechunk ChunkReconfigurer, newModel ChatModelFactory,
	initial settingsdom.Values,
) *Service {
	return &Service{
		repo: repo, domain: domain, admin: admin, indexUID: indexUID,
		reconfigure: reconfigure, rechunk: rechunk, newModel: newModel, current: initial,
	}
}

// Get 返回对外呈现的设置(隐藏 API key)。
func (s *Service) Get() settingsdom.Values {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.domain.Public(s.current)
}

// Raw 返回含密钥的完整设置;仅供进程内装配(如联网搜索工厂)使用,不得透出到 HTTP 层。
func (s *Service) Raw() settingsdom.Values {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// Update 校验并应用新设置:先更新检索引擎 embedder,再持久化,最后热切换聊天模型。
func (s *Service) Update(ctx context.Context, next settingsdom.Values) (settingsdom.Values, error) {
	s.domain.Normalize(&next)
	if err := s.domain.Validate(next); err != nil {
		return settingsdom.Values{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next.ChatModels = s.domain.MergeAPIKeys(s.current.ChatModels, next.ChatModels)
	s.domain.MergeAnySearchKey(s.current, &next)
	if next.WebSearchEnabled && next.WebSearchProvider == "anysearch" && next.AnySearchAPIKey == "" {
		return settingsdom.Values{}, fmt.Errorf("anysearch provider requires an API key")
	}
	if err := s.admin.EnsureIndex(ctx, s.indexUID, port.EmbedderConfig{
		URL: next.EmbedderURL, Model: next.EmbedderModel, Dimensions: next.EmbedderDimensions,
	}); err != nil {
		return settingsdom.Values{}, fmt.Errorf("apply embedder settings: %w", err)
	}
	if err := s.repo.Save(ctx, s.domain.Encode(next)); err != nil {
		return settingsdom.Values{}, err
	}
	s.current = next
	s.rechunk.SetChunker(media.NewChunkerWithSeparators(next.ChunkSize, next.ChunkOverlap, next.ChunkSeparators))
	models := make(map[string]port.ChatModel, len(next.ChatModels))
	for _, configured := range next.ChatModels {
		models[configured.ModelBizID] = s.newModel(configured.Protocol, configured.BaseURL, configured.APIKey, configured.Model)
	}
	s.reconfigure.SetModels(models, next.DefaultChatModelBizID)
	return s.domain.Public(next), nil
}
