package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"open-ima/internal/application/port"
	"open-ima/internal/domain/conversation"
	"open-ima/internal/domain/media"
	"open-ima/internal/infrastructure/db"
)

// fakeSearcher 记录请求并返回预置命中。
type fakeSearcher struct {
	requests []port.SearchRequest
	hits     []port.SearchHit
	err      error
}

func (f *fakeSearcher) Search(_ context.Context, _ string, request port.SearchRequest) ([]port.SearchHit, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return nil, f.err
	}
	return f.hits, nil
}

type fakeWebSearcher struct{ results []port.WebResult }

func (f *fakeWebSearcher) Search(_ context.Context, _ string, _ int) ([]port.WebResult, error) {
	return f.results, nil
}

func newMediaService(t *testing.T) *media.MediaService {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO knowledge_bases (kb_biz_id, name) VALUES ('kb1', '库')`); err != nil {
		t.Fatal(err)
	}
	return media.NewMediaService(db.NewMediaRepository(database))
}

func TestRegistryFirstWinsAndUnknown(t *testing.T) {
	registry := NewRegistry(16000)
	first := NewWebSearch(&fakeWebSearcher{}, 5, NewHandles())
	second := NewListDocuments(nil, "kb1", NewHandles())
	registry.Register(first)
	registry.Register(first)
	registry.Register(second)
	defs := registry.Defs()
	if len(defs) != 2 || defs[0].Name != "web_search" {
		t.Fatalf("defs = %+v", defs)
	}
	result := registry.Execute(context.Background(), "missing", json.RawMessage(`{}`))
	if result.Success || !strings.Contains(result.Error, "unknown tool") {
		t.Fatalf("result = %+v", result)
	}
}

type panicTool struct{}

func (panicTool) Name() string                { return "boom" }
func (panicTool) Description() string         { return "" }
func (panicTool) Parameters() json.RawMessage { return json.RawMessage(`{}`) }
func (panicTool) Execute(context.Context, json.RawMessage) (*port.ToolResult, error) {
	panic("kaboom")
}

func TestRegistryRecoversPanic(t *testing.T) {
	registry := NewRegistry(16000)
	registry.Register(panicTool{})
	result := registry.Execute(context.Background(), "boom", json.RawMessage(`{}`))
	if result.Success || !strings.Contains(result.Error, "panicked") {
		t.Fatalf("result = %+v", result)
	}
}

type verboseTool struct{ size int }

func (t verboseTool) Name() string                { return "verbose" }
func (t verboseTool) Description() string         { return "" }
func (t verboseTool) Parameters() json.RawMessage { return json.RawMessage(`{}`) }
func (t verboseTool) Execute(context.Context, json.RawMessage) (*port.ToolResult, error) {
	return &port.ToolResult{Success: true, Output: strings.Repeat("字", t.size)}, nil
}

func TestRegistryTruncatesHeadTail(t *testing.T) {
	registry := NewRegistry(100)
	registry.Register(verboseTool{size: 500})
	result := registry.Execute(context.Background(), "verbose", json.RawMessage(`{}`))
	runes := []rune(result.Output)
	if !strings.Contains(result.Output, "已截断") || len(runes) > 140 {
		t.Fatalf("output length = %d", len(runes))
	}
}

func TestSearchKnowledgeModesAndHandles(t *testing.T) {
	searcher := &fakeSearcher{hits: []port.SearchHit{
		{ID: "chunk1", MediaBizID: "doc1", Title: "文档一", Content: "内容一", Score: 0.9},
		{ID: "chunk2", MediaBizID: "doc1", Title: "文档一", Content: "内容二", Score: 0.8},
	}}
	handles := NewHandles()
	tool := NewSearchKnowledge(searcher, "chunks", "kb1", handles)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"什么是甲","mode":"semantic"}`))
	if err != nil || !result.Success {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !searcher.requests[0].Hybrid {
		t.Fatal("semantic should fall back to hybrid")
	}
	if !strings.Contains(result.Output, "mode fallback") {
		t.Fatalf("fallback must be explicit: %q", result.Output)
	}
	if searcher.requests[0].Filter != "kb_biz_id = 'kb1'" || searcher.requests[0].Limit != 10 {
		t.Fatalf("request = %+v", searcher.requests[0])
	}
	if !strings.Contains(result.Output, "[c1]") || !strings.Contains(result.Output, "文档 d1") {
		t.Fatalf("output = %q", result.Output)
	}
	citations, _ := result.Data["citations"].([]conversation.Citation)
	if len(citations) != 2 || citations[0].SourceType != conversation.SourceTypeKBChunk || citations[0].ChunkBizID != "chunk1" {
		t.Fatalf("citations = %+v", citations)
	}
	newHandles, _ := result.Data["handles"].(map[string]string)
	if newHandles["c1"] != "chunk1" || newHandles["d1"] != "doc1" {
		t.Fatalf("handles = %+v", newHandles)
	}
	// 同一分块重复检索不重复分配句柄
	result, _ = tool.Execute(context.Background(), json.RawMessage(`{"query":"再问","mode":"keyword"}`))
	newHandles, _ = result.Data["handles"].(map[string]string)
	if len(newHandles) != 0 {
		t.Fatalf("re-assign handles = %+v", newHandles)
	}
	if searcher.requests[1].Hybrid {
		t.Fatal("keyword mode must not use hybrid")
	}
}

func TestSearchKnowledgeRejectsBadInput(t *testing.T) {
	tool := NewSearchKnowledge(&fakeSearcher{}, "chunks", "kb1", NewHandles())
	result, _ := tool.Execute(context.Background(), json.RawMessage(`{"query":" "}`))
	if result.Success {
		t.Fatal("empty query must fail")
	}
	result, _ = tool.Execute(context.Background(), json.RawMessage(`{"query":"q","mode":"bogus"}`))
	if result.Success || !strings.Contains(result.Error, "unsupported mode") {
		t.Fatalf("result = %+v", result)
	}
}

func TestReadDocumentByMediaHandle(t *testing.T) {
	docs := newMediaService(t)
	ctx := context.Background()
	mediaBizID, _, err := docs.Create(ctx, "kb1", "长文档", "file", "files/x", "md", "")
	if err != nil {
		t.Fatal(err)
	}
	chunks := make([]media.StoredChunk, 3)
	for index := range chunks {
		chunks[index] = media.StoredChunk{BizID: string(rune('a'+index)) + "-chunk", Seq: index}
	}
	if err := docs.ReplaceChunks(ctx, mediaBizID, chunks); err != nil {
		t.Fatal(err)
	}
	searcher := &fakeSearcher{hits: []port.SearchHit{
		{ID: "a-chunk", MediaBizID: mediaBizID, Content: "第一段"},
		{ID: "b-chunk", MediaBizID: mediaBizID, Content: "第二段"},
		{ID: "c-chunk", MediaBizID: mediaBizID, Content: "第三段"},
	}}
	handles := NewHandles()
	handles.Assign("d", mediaBizID)
	tool := NewReadDocument(docs, searcher, "chunks", handles)

	result, err := tool.Execute(ctx, json.RawMessage(`{"id":"d1","offset":1,"limit":1}`))
	if err != nil || !result.Success {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !strings.Contains(result.Output, "第二段") || strings.Contains(result.Output, "第一段") {
		t.Fatalf("output = %q", result.Output)
	}
	if !strings.Contains(result.Output, "还有 1 分块") {
		t.Fatalf("continuation hint missing: %q", result.Output)
	}
}

func TestReadDocumentByChunkHandle(t *testing.T) {
	docs := newMediaService(t)
	ctx := context.Background()
	mediaBizID, _, _ := docs.Create(ctx, "kb1", "文档", "file", "files/x", "md", "")
	_ = docs.ReplaceChunks(ctx, mediaBizID, []media.StoredChunk{{BizID: "chunk-9", Seq: 0}})
	searcher := &fakeSearcher{hits: []port.SearchHit{{ID: "chunk-9", MediaBizID: mediaBizID, Content: "目标内容"}}}
	handles := NewHandles()
	handles.Assign("c", "chunk-9")
	handles.LinkChunkMedia("chunk-9", mediaBizID)
	tool := NewReadDocument(docs, searcher, "chunks", handles)
	result, err := tool.Execute(ctx, json.RawMessage(`{"id":"c1"}`))
	if err != nil || !result.Success || !strings.Contains(result.Output, "目标内容") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestListDocumentsFilterAndPagination(t *testing.T) {
	docs := newMediaService(t)
	ctx := context.Background()
	for _, title := range []string{"年度报告", "月度小结", "年度规划"} {
		if _, _, err := docs.Create(ctx, "kb1", title, "file", "files/"+title, "md", ""); err != nil {
			t.Fatal(err)
		}
	}
	handles := NewHandles()
	tool := NewListDocuments(docs, "kb1", handles)
	result, err := tool.Execute(ctx, json.RawMessage(`{"keyword":"年度"}`))
	if err != nil || !result.Success {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !strings.Contains(result.Output, "共 2 个文档") || !strings.Contains(result.Output, "[d1]") {
		t.Fatalf("output = %q", result.Output)
	}
	if strings.Contains(result.Output, "月度小结") {
		t.Fatalf("keyword filter failed: %q", result.Output)
	}
}

func TestWebSearchTool(t *testing.T) {
	tool := NewWebSearch(&fakeWebSearcher{results: []port.WebResult{
		{Title: "示例", URL: "https://example.com/a", Snippet: "摘要"},
	}}, 5, NewHandles())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"news"}`))
	if err != nil || !result.Success {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	citations, _ := result.Data["citations"].([]conversation.Citation)
	if len(citations) != 1 || citations[0].SourceType != conversation.SourceTypeWeb || citations[0].URL != "https://example.com/a" {
		t.Fatalf("citations = %+v", citations)
	}
	if !strings.Contains(result.Output, "[w1] 示例") {
		t.Fatalf("output = %q", result.Output)
	}
}
