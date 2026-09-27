// Package app 是唯一的装配根:加载配置、打开数据库、组装领域服务、
// 应用用例、基础设施实现与 HTTP 入站适配。
package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"path/filepath"

	"open-ima/internal/application/chat"
	"open-ima/internal/application/ingest"
	kbapp "open-ima/internal/application/knowledgebase"
	"open-ima/internal/application/port"
	"open-ima/internal/application/reading"
	settingsapp "open-ima/internal/application/settings"
	"open-ima/internal/domain/conversation"
	"open-ima/internal/domain/media"
	kbdom "open-ima/internal/domain/knowledgebase"
	settingsdom "open-ima/internal/domain/settings"
	"open-ima/internal/infrastructure/config"
	"open-ima/internal/infrastructure/db"
	"open-ima/internal/infrastructure/fetch"
	"open-ima/internal/infrastructure/llm"
	"open-ima/internal/infrastructure/meili"
	"open-ima/internal/infrastructure/parser"
	"open-ima/internal/infrastructure/queue"
	"open-ima/internal/infrastructure/storage"
	"open-ima/internal/infrastructure/system"
	httpapi "open-ima/internal/interfaces/http"
	frontend "open-ima/web"
)

type App struct {
	Handler http.Handler
	Worker  *queue.Worker
	Ingest  *ingest.Service
}

// New 组装应用:先以持久化设置覆盖配置,再逐一构建各层组件。
func New(cfg *config.Config, database *sql.DB) (*App, error) {
	settingsDomain := settingsdom.NewSettingsService()
	settingsRepo := db.NewSettingsRepository(database)
	stored, err := settingsRepo.Load(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	current, err := settingsDomain.Overlay(valuesFromConfig(cfg), stored)
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	applyValues(cfg, current)

	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	store, err := storage.NewLocalStorage(
		filepath.Join(cfg.DataDir, "files"), cfg.PublicBaseURL, hex.EncodeToString(secret),
	)
	if err != nil {
		return nil, err
	}
	meiliClient := meili.New(cfg.Meili.URL, cfg.Meili.APIKey)
	if err := meiliClient.EnsureIndex(context.Background(), cfg.Meili.Index, port.EmbedderConfig{
		URL: cfg.Meili.EmbedderURL, Model: cfg.Meili.EmbedderModel, Dimensions: cfg.Meili.EmbedderDimensions,
	}); err != nil {
		return nil, fmt.Errorf("meilisearch ensure index: %w", err)
	}

	kbService := kbdom.NewKBService(db.NewKnowledgeBaseRepository(database))
	documentService := media.NewMediaService(db.NewDocumentRepository(database))
	conversationService := conversation.NewConversationService(db.NewConversationRepository(database))

	jobQueue := queue.New(database)
	ingestService := ingest.NewService(
		documentService, kbService, jobQueue, store,
		parser.New(cfg.Parser.URL), meiliClient, media.NewChunker(512, 80), cfg.Meili.Index,
	)
	worker := queue.NewWorker(jobQueue)
	ingestService.RegisterHandlers(worker)

	knowledgeBaseService := kbapp.NewService(
		kbService, documentService, conversationService, ingestService, store, fetch.New(),
	)
	chatModelFactory := func(protocol, baseURL, apiKey, model string) port.ChatModel {
		return llm.NewChatClientWithProtocol(protocol, baseURL, apiKey, model)
	}
	chatService := chat.NewService(
		conversationService, kbService, meiliClient,
		chatModelFactory(cfg.LLM.Protocol, cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model), cfg.Meili.Index,
	)
	opener, err := system.NewOpener()
	if err != nil {
		return nil, err
	}
	readingService := reading.NewService(documentService, meiliClient, store, opener, cfg.Meili.Index)
	configuredModels := make(map[string]port.ChatModel, len(current.ChatModels))
	for _, configured := range current.ChatModels {
		configuredModels[configured.ModelBizID] = chatModelFactory(configured.Protocol, configured.BaseURL, configured.APIKey, configured.Model)
	}
	chatService.SetModels(configuredModels, current.DefaultChatModelBizID)
	settingsService := settingsapp.NewService(
		settingsRepo, settingsDomain, meiliClient, cfg.Meili.Index, chatService, chatModelFactory, current,
	)

	mux := httpapi.NewRouter(httpapi.Deps{
		KnowledgeBase: knowledgeBaseService, Ingest: ingestService,
		Chat: chatService, Reading: readingService, Settings: settingsService, Store: store,
	})
	mux.Handle("GET /internal/files/{key}", store.Handler())
	mux.Handle("/", frontend.Handler())

	return &App{Handler: mux, Worker: worker, Ingest: ingestService}, nil
}

// valuesFromConfig 将启动配置映射为设置取值;持久化的键值随后覆盖它。
func valuesFromConfig(cfg *config.Config) settingsdom.Values {
	return settingsdom.Values{
		ChatModels: []settingsdom.ChatModel{{ModelBizID: settingsdom.DefaultModelBizID, Name: cfg.LLM.Model,
			Protocol: cfg.LLM.Protocol, BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey, Model: cfg.LLM.Model}},
		DefaultChatModelBizID: settingsdom.DefaultModelBizID, EmbedderURL: cfg.Meili.EmbedderURL,
		EmbedderModel: cfg.Meili.EmbedderModel, EmbedderDimensions: cfg.Meili.EmbedderDimensions,
	}
}

// applyValues 将生效设置回写到配置,供其余组件按原路径读取。
func applyValues(cfg *config.Config, values settingsdom.Values) {
	for _, model := range values.ChatModels {
		if model.ModelBizID == values.DefaultChatModelBizID {
			cfg.LLM.Protocol, cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model = model.Protocol, model.BaseURL, model.APIKey, model.Model
			break
		}
	}
	cfg.Meili.EmbedderURL, cfg.Meili.EmbedderModel, cfg.Meili.EmbedderDimensions =
		values.EmbedderURL, values.EmbedderModel, values.EmbedderDimensions
}
