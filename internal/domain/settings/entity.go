// Package settings 承载应用设置聚合:取值实体、持久化契约与校验规则。
package settings

// Values 是应用设置的取值对象。LLMAPIKey 仅在写入时携带,读出时以
// APIKeyConfigured 表示是否已配置;ClearAPIKey 为写入时的清除指令。
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

// 持久化键名;仓储实现与装配根共用,避免魔法字符串散落。
const (
	KeyLLMProtocol        = "llm.protocol"
	KeyLLMBaseURL         = "llm.base_url"
	KeyLLMAPIKey          = "llm.api_key"
	KeyLLMModel           = "llm.model"
	KeyEmbedderURL        = "meili.embedder_url"
	KeyEmbedderModel      = "meili.embedder_model"
	KeyEmbedderDimensions = "meili.embedder_dimensions"
)
