// Package settings 承载应用设置聚合:取值实体、持久化契约与校验规则。
package settings

// ChatModel 是一个可选的问答模型配置。APIKey 仅在写入时携带，读取时以
// APIKeyConfigured 表示是否已配置；ClearAPIKey 为写入时的清除指令。
type ChatModel struct {
	ModelBizID       string `json:"model_biz_id"`
	Name             string `json:"name"`
	Protocol         string `json:"protocol"`
	BaseURL          string `json:"base_url"`
	Model            string `json:"model"`
	APIKey           string `json:"api_key,omitempty"`
	APIKeyConfigured bool   `json:"api_key_configured"`
	ClearAPIKey      bool   `json:"clear_api_key,omitempty"`
}

// Values 是应用设置的取值对象。
type Values struct {
	ChatModels            []ChatModel `json:"chat_models"`
	DefaultChatModelBizID string      `json:"default_chat_model_biz_id"`
	EmbedderURL           string      `json:"embedder_url"`
	EmbedderModel         string      `json:"embedder_model"`
	EmbedderDimensions    int         `json:"embedder_dimensions"`
}

// 持久化键名;仓储实现与装配根共用,避免魔法字符串散落。
const (
	KeyChatModels            = "llm.models"
	KeyDefaultChatModelBizID = "llm.default_model_biz_id"
	KeyLLMProtocol           = "llm.protocol"
	KeyLLMBaseURL            = "llm.base_url"
	KeyLLMAPIKey             = "llm.api_key"
	KeyLLMModel              = "llm.model"
	KeyEmbedderURL           = "meili.embedder_url"
	KeyEmbedderModel         = "meili.embedder_model"
	KeyEmbedderDimensions    = "meili.embedder_dimensions"
)

const DefaultModelBizID = "00000000-0000-4000-8000-000000000001"
