package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"open-ima/internal/application/port"
	"open-ima/internal/domain/conversation"
)

// WebSearch 包装 port.WebSearcher;仅在设置开启联网搜索时注入注册表。
// 结果为每条 URL 分配全局 wN 句柄,供答案内引用编号对齐。
type WebSearch struct {
	searcher     port.WebSearcher
	defaultLimit int
	handles      *Handles
}

func NewWebSearch(searcher port.WebSearcher, defaultLimit int, handles *Handles) *WebSearch {
	if defaultLimit <= 0 {
		defaultLimit = 5
	}
	return &WebSearch{searcher: searcher, defaultLimit: defaultLimit, handles: handles}
}

func (t *WebSearch) Name() string { return "web_search" }

func (t *WebSearch) Description() string {
	return "联网搜索公开网页。当知识库内容不足以回答、或问题涉及时效性信息时使用;" +
		"用简洁的关键词组合查询。结果中的 URL 可作为引用来源。"
}

func (t *WebSearch) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "搜索关键词"},
			"limit": {"type": "integer", "description": "返回条数,默认取设置值"}
		},
		"required": ["query"]
	}`)
}

func (t *WebSearch) Execute(ctx context.Context, args json.RawMessage) (*port.ToolResult, error) {
	var parsed struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		return nil, fmt.Errorf("web_search: invalid arguments: %w", err)
	}
	parsed.Query = strings.TrimSpace(parsed.Query)
	if parsed.Query == "" {
		return &port.ToolResult{Success: false, Error: "query is required"}, nil
	}
	if parsed.Limit <= 0 {
		parsed.Limit = t.defaultLimit
	}
	results, err := t.searcher.Search(ctx, parsed.Query, parsed.Limit)
	if err != nil {
		return nil, err
	}
	var output strings.Builder
	if len(results) == 0 {
		output.WriteString("无结果。请改写关键词重试。")
	}
	citations := make([]conversation.Citation, 0, len(results))
	newHandles := make(map[string]string)
	for _, result := range results {
		handle, created := t.handles.Assign("w", result.URL)
		if created {
			newHandles[handle] = result.URL
		}
		fmt.Fprintf(&output, "[%s] %s\n%s\n%s\n\n", handle, result.Title, result.URL, result.Snippet)
		citations = append(citations, conversation.Citation{
			SourceType: conversation.SourceTypeWeb,
			Title:      result.Title, URL: result.URL, Snippet: result.Snippet,
		})
	}
	return &port.ToolResult{
		Success: true,
		Output:  strings.TrimRight(output.String(), "\n"),
		Data:    map[string]any{"citations": citations, "handles": newHandles},
	}, nil
}
