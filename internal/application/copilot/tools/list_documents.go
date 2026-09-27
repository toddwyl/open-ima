package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"open-ima/internal/application/port"
	"open-ima/internal/domain/media"
)

// list_documents 每页条数。
const listDocumentsPageSize = 20

// ListDocuments 浏览当前知识库的文档列表,支持标题关键词过滤与分页;
// 结果为文档分配 dN 句柄。
type ListDocuments struct {
	docs    *media.MediaService
	kbBizID string
	handles *Handles
}

func NewListDocuments(docs *media.MediaService, kbBizID string, handles *Handles) *ListDocuments {
	return &ListDocuments{docs: docs, kbBizID: kbBizID, handles: handles}
}

func (t *ListDocuments) Name() string { return "list_documents" }

func (t *ListDocuments) Description() string {
	return "浏览当前知识库中的文档列表,可按标题关键词过滤。当不确定答案在哪个文档、或需要按标题找文档时使用;" +
		"返回的 dN 句柄可传给 read_document 读正文。"
}

func (t *ListDocuments) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"keyword": {"type": "string", "description": "标题关键词过滤,可选"},
			"page": {"type": "integer", "description": "页码,从 1 开始,默认 1"}
		}
	}`)
}

func (t *ListDocuments) Execute(ctx context.Context, args json.RawMessage) (*port.ToolResult, error) {
	var parsed struct {
		Keyword string `json:"keyword"`
		Page    int    `json:"page"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		return nil, fmt.Errorf("list_documents: invalid arguments: %w", err)
	}
	if parsed.Page <= 0 {
		parsed.Page = 1
	}
	documents, err := t.docs.List(ctx, t.kbBizID)
	if err != nil {
		return nil, err
	}
	keyword := strings.ToLower(strings.TrimSpace(parsed.Keyword))
	filtered := make([]media.Media, 0, len(documents))
	for _, document := range documents {
		if keyword != "" && !strings.Contains(strings.ToLower(document.Title), keyword) {
			continue
		}
		filtered = append(filtered, document)
	}
	start := (parsed.Page - 1) * listDocumentsPageSize
	if start >= len(filtered) && len(filtered) > 0 {
		return &port.ToolResult{Success: true, Output: fmt.Sprintf("第 %d 页为空(共 %d 个文档)。", parsed.Page, len(filtered))}, nil
	}
	end := min(start+listDocumentsPageSize, len(filtered))
	var output strings.Builder
	fmt.Fprintf(&output, "共 %d 个文档(第 %d 页):\n", len(filtered), parsed.Page)
	newHandles := make(map[string]string)
	for _, document := range filtered[start:end] {
		handle, created := t.handles.Assign("d", document.BizID)
		if created {
			newHandles[handle] = document.BizID
		}
		fmt.Fprintf(&output, "[%s]《%s》状态=%s 分块数=%d\n", handle, document.Title, document.Status, document.ChunkCount)
	}
	if end < len(filtered) {
		fmt.Fprintf(&output, "(还有 %d 个文档,用 page=%d 翻页)", len(filtered)-end, parsed.Page+1)
	}
	return &port.ToolResult{
		Success: true,
		Output:  strings.TrimRight(output.String(), "\n"),
		Data:    map[string]any{"handles": newHandles},
	}, nil
}
