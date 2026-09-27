package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"open-ima/internal/application/port"
	"open-ima/internal/domain/conversation"
)

// search_knowledge 的参数与默认上限。
const searchKnowledgeDefaultLimit = 10

// SearchKnowledge 包装 port.Searcher,在当前知识库范围内检索分块;
// 结果为媒体/分块分配 dN/cN 句柄并附引用数据。
type SearchKnowledge struct {
	searcher port.Searcher
	index    string
	kbBizID  string
	handles  *Handles
}

func NewSearchKnowledge(searcher port.Searcher, index, kbBizID string, handles *Handles) *SearchKnowledge {
	return &SearchKnowledge{searcher: searcher, index: index, kbBizID: kbBizID, handles: handles}
}

func (t *SearchKnowledge) Name() string { return "search_knowledge" }

// Description 即检索指南:把检索经验固化在工具描述里。
func (t *SearchKnowledge) Description() string {
	return "在当前知识库中检索相关分块。写完整自然语言问句而非关键词串;" +
		"查标识符/错误码等精确字符串时改用 keyword 模式重试;无结果时换成文档更可能使用的术语改写再问。" +
		"结果中的 cN 是分块句柄、dN 是文档句柄,可用 read_document 读取正文。"
}

func (t *SearchKnowledge) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "完整自然语言问句"},
			"mode": {"type": "string", "enum": ["hybrid", "keyword", "semantic"], "description": "检索模式,默认 hybrid"},
			"limit": {"type": "integer", "description": "返回条数,默认 10"}
		},
		"required": ["query"]
	}`)
}

func (t *SearchKnowledge) Execute(ctx context.Context, args json.RawMessage) (*port.ToolResult, error) {
	var parsed struct {
		Query string `json:"query"`
		Mode  string `json:"mode"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		return nil, fmt.Errorf("search_knowledge: invalid arguments: %w", err)
	}
	parsed.Query = strings.TrimSpace(parsed.Query)
	if parsed.Query == "" {
		return &port.ToolResult{Success: false, Error: "query is required"}, nil
	}
	if parsed.Limit <= 0 {
		parsed.Limit = searchKnowledgeDefaultLimit
	}
	request := port.SearchRequest{
		Query: parsed.Query, Filter: "kb_biz_id = '" + escapeFilter(t.kbBizID) + "'", Limit: parsed.Limit,
	}
	var modeNote string
	switch parsed.Mode {
	case "", "hybrid":
		request.Hybrid = true
	case "keyword":
		// keyword 映射为纯文本检索
	case "semantic":
		// 引擎不支持纯语义检索时以 hybrid 兜底,并显式注明(不静默降级)
		request.Hybrid = true
		modeNote = "mode fallback: semantic 暂以 hybrid 执行。"
	default:
		return &port.ToolResult{Success: false, Error: fmt.Sprintf("unsupported mode %q", parsed.Mode)}, nil
	}
	hits, err := t.searcher.Search(ctx, t.index, request)
	if err != nil {
		return nil, err
	}
	var output strings.Builder
	if modeNote != "" {
		output.WriteString(modeNote + "\n")
	}
	if len(hits) == 0 {
		output.WriteString("无结果。请换用文档更可能使用的术语改写查询,或改用 keyword 模式重试。")
	}
	citations := make([]conversation.Citation, 0, len(hits))
	newHandles := make(map[string]string)
	for _, hit := range hits {
		chunkHandle, created := t.handles.Assign("c", hit.ID)
		if created {
			newHandles[chunkHandle] = hit.ID
		}
		mediaHandle, created := t.handles.Assign("d", hit.MediaBizID)
		if created {
			newHandles[mediaHandle] = hit.MediaBizID
		}
		t.handles.LinkChunkMedia(hit.ID, hit.MediaBizID)
		snippet := hit.Formatted
		if snippet == "" {
			snippet = hit.Content
		}
		fmt.Fprintf(&output, "[%s]《%s》(文档 %s)\n%s\n\n", chunkHandle, hit.Title, mediaHandle, snippet)
		citations = append(citations, conversation.Citation{
			SourceType: conversation.SourceTypeKBChunk,
			MediaBizID: hit.MediaBizID, ChunkBizID: hit.ID,
			Title: hit.Title, Snippet: snippet, Score: hit.Score,
		})
	}
	return &port.ToolResult{
		Success: true,
		Output:  strings.TrimRight(output.String(), "\n"),
		Data:    map[string]any{"citations": citations, "handles": newHandles},
	}, nil
}

func escapeFilter(value string) string { return strings.ReplaceAll(value, "'", "''") }
