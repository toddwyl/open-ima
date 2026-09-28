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
	ChunkSize             int         `json:"chunk_size"`
	ChunkOverlap          int         `json:"chunk_overlap"`
	ChunkSeparators       []string    `json:"chunk_separators"`
	WebSearchEnabled      bool        `json:"web_search_enabled"`
	WebSearchMaxResults   int         `json:"web_search_max_results"`
	// AnySearch API key 仅在写入时携带,读取时以 Configured 表示;Clear 为清除指令。
	AnySearchAPIKey           string `json:"anysearch_api_key,omitempty"`
	AnySearchAPIKeyConfigured bool   `json:"anysearch_api_key_configured"`
	ClearAnySearchAPIKey      bool   `json:"clear_anysearch_api_key,omitempty"`
}

// 联网搜索默认值;provider 固定为 AnySearch(结构化 API,需在配置中心填 key)。
const DefaultWebSearchMaxResults = 5

// 分块默认值;与 infrastructure/config 的默认保持一致,作为设置未配置时的兜底。
const (
	DefaultChunkSize    = 512
	DefaultChunkOverlap = 80
)

// DefaultChunkSeparators 返回默认递归分隔符(降序优先级)。
func DefaultChunkSeparators() []string {
	return []string{"\n\n", "\n", "。", "?", "!", ";", " "}
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
	KeyChunkSize             = "chunk.size"
	KeyChunkOverlap          = "chunk.overlap"
	KeyChunkSeparators       = "chunk.separators"
	KeyWebSearchEnabled      = "web_search.enabled"
	KeyWebSearchMaxResults   = "web_search.max_results"
	KeyAnySearchAPIKey       = "web_search.anysearch_api_key"
)

const DefaultModelBizID = "00000000-0000-4000-8000-000000000001"
