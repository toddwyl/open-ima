package port

import "context"

// Block 是解析结果中的内容块。
type Block struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Level int    `json:"level"`
}

// ParseResult 是 parser sidecar 的解析产物。
type ParseResult struct {
	Title  string  `json:"title"`
	Blocks []Block `json:"blocks"`
}

// FatalError 表示输入无法解析,重试无意义。
type FatalError struct{ Message string }

func (e *FatalError) Error() string { return e.Message }

// Parser 是文档解析能力端口。
type Parser interface {
	Parse(ctx context.Context, fileURL, fileType string) (*ParseResult, error)
}
