package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"open-ima/internal/application/port"
	"open-ima/internal/domain/media"
)

// read_document 默认读取的分块数与上限。
const (
	readDocumentDefaultLimit = 10
	readDocumentMaxLimit     = 50
)

// ReadDocument 按文档或分块句柄读取正文;正文由检索引擎托管,
// 顺序取自元数据库的 seq(与阅读视图同一拼接规则)。
type ReadDocument struct {
	docs     *media.MediaService
	searcher port.Searcher
	index    string
	handles  *Handles
}

func NewReadDocument(docs *media.MediaService, searcher port.Searcher, index string, handles *Handles) *ReadDocument {
	return &ReadDocument{docs: docs, searcher: searcher, index: index, handles: handles}
}

func (t *ReadDocument) Name() string { return "read_document" }

func (t *ReadDocument) Description() string {
	return "读取文档或分块的正文。id 接受 search_knowledge / list_documents 返回的 dN(文档)或 cN(分块)句柄;" +
		"传 query 时在文档内检索定位相关片段;offset/limit 用于分段翻阅长文档。"
}

func (t *ReadDocument) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"id": {"type": "string", "description": "文档句柄 dN 或分块句柄 cN"},
			"query": {"type": "string", "description": "可选,文内定位关键词"},
			"offset": {"type": "integer", "description": "起始分块序号,默认 0"},
			"limit": {"type": "integer", "description": "读取分块数,默认 10,最大 50"}
		},
		"required": ["id"]
	}`)
}

func (t *ReadDocument) Execute(ctx context.Context, args json.RawMessage) (*port.ToolResult, error) {
	var parsed struct {
		ID     string `json:"id"`
		Query  string `json:"query"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		return nil, fmt.Errorf("read_document: invalid arguments: %w", err)
	}
	parsed.ID = strings.TrimSpace(parsed.ID)
	if parsed.ID == "" {
		return &port.ToolResult{Success: false, Error: "id is required"}, nil
	}
	if parsed.Offset < 0 {
		parsed.Offset = 0
	}
	if parsed.Limit <= 0 {
		parsed.Limit = readDocumentDefaultLimit
	}
	if parsed.Limit > readDocumentMaxLimit {
		parsed.Limit = readDocumentMaxLimit
	}
	resolved := t.handles.Resolve(parsed.ID)
	mediaBizID, chunkBizID := resolved, ""
	if strings.HasPrefix(parsed.ID, "c") && parsed.ID != resolved {
		chunkBizID = resolved
		var ok bool
		mediaBizID, ok = t.handles.ChunkMedia(resolved)
		if !ok {
			return &port.ToolResult{Success: false, Error: fmt.Sprintf("chunk handle %q has no media mapping", parsed.ID)}, nil
		}
	}
	return t.read(ctx, mediaBizID, chunkBizID, parsed.Query, parsed.Offset, parsed.Limit)
}

func (t *ReadDocument) read(ctx context.Context, mediaBizID, chunkBizID, query string, offset, limit int) (*port.ToolResult, error) {
	doc, err := t.docs.Get(ctx, mediaBizID)
	if err != nil {
		return &port.ToolResult{Success: false, Error: fmt.Sprintf("document %s not found", mediaBizID)}, nil
	}
	chunks, err := t.docs.ListChunks(ctx, mediaBizID)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return &port.ToolResult{Success: true, Output: "文档尚无已索引的正文。"}, nil
	}
	hits, err := t.searcher.Search(ctx, t.index, port.SearchRequest{
		Query: strings.TrimSpace(query), Filter: "media_biz_id = '" + escapeFilter(mediaBizID) + "'", Limit: len(chunks),
	})
	if err != nil {
		return nil, err
	}
	contentByID := make(map[string]string, len(hits))
	for _, hit := range hits {
		contentByID[hit.ID] = hit.Content
	}
	mediaHandle, _ := t.handles.Assign("d", doc.BizID)
	var output strings.Builder
	fmt.Fprintf(&output, "《%s》(文档 %s,共 %d 分块)\n", doc.Title, mediaHandle, len(chunks))
	switch {
	case chunkBizID != "":
		output.WriteString(chunkText(contentByID, chunkBizID))
	default:
		if query != "" && len(hits) > 0 {
			// query 定位:按检索命中顺序返回
			shown := 0
			for _, hit := range hits {
				if shown >= limit {
					break
				}
				fmt.Fprintf(&output, "\n[分块 %s]\n%s\n", hit.ID, hit.Content)
				shown++
			}
			break
		}
		end := min(offset+limit, len(chunks))
		if offset >= len(chunks) {
			fmt.Fprintf(&output, "offset %d 超出范围(共 %d 分块)。", offset, len(chunks))
			break
		}
		for _, chunk := range chunks[offset:end] {
			fmt.Fprintf(&output, "\n[分块 %d/%d %s]\n%s\n", chunk.Seq, len(chunks), chunk.BizID, contentByID[chunk.BizID])
		}
		if end < len(chunks) {
			fmt.Fprintf(&output, "\n(还有 %d 分块,用 offset=%d 继续阅读)", len(chunks)-end, end)
		}
	}
	return &port.ToolResult{Success: true, Output: strings.TrimSpace(output.String())}, nil
}

func chunkText(contentByID map[string]string, chunkBizID string) string {
	content, ok := contentByID[chunkBizID]
	if !ok {
		return fmt.Sprintf("分块 %s 未找到正文。", chunkBizID)
	}
	return content
}
