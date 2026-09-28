package copilot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"open-ima/internal/application/copilot/tools"
	"open-ima/internal/application/port"
	"open-ima/internal/domain/conversation"
	"open-ima/internal/domain/knowledgebase"
	"open-ima/internal/domain/media"
	settingsdom "open-ima/internal/domain/settings"
	"open-ima/internal/infrastructure/db"
)

// rigSearcher 是 service 测试用的确定性检索器。
type rigSearcher struct {
	hits []port.SearchHit
}

func (r *rigSearcher) Search(_ context.Context, _ string, _ port.SearchRequest) ([]port.SearchHit, error) {
	return r.hits, nil
}

type serviceRig struct {
	service *Service
	kbbiz   string
}

func newServiceRig(t *testing.T, model port.ChatModel, settings settingsdom.Values) *serviceRig {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO knowledge_bases (kb_biz_id, name) VALUES ('kb1', '库')`); err != nil {
		t.Fatal(err)
	}
	searcher := &rigSearcher{hits: []port.SearchHit{
		{ID: "chunk-1", MediaBizID: "doc-1", Title: "文档一", Content: "甲的内容", Score: 0.9},
	}}
	service := NewService(
		conversation.NewConversationService(db.NewConversationRepository(database)),
		knowledgebase.NewKBService(db.NewKnowledgeBaseRepository(database)),
		media.NewMediaService(db.NewMediaRepository(database)),
		searcher, "chunks", model, DefaultGuards(),
		func() settingsdom.Values { return settings },
		func(settingsdom.Values) (port.WebSearcher, error) {
			return &fakeWebSearcher{results: []port.WebResult{{Title: "网页", URL: "https://example.com", Snippet: "网摘"}}}, nil
		},
	)
	return &serviceRig{service: service, kbbiz: "kb1"}
}

type fakeWebSearcher struct{ results []port.WebResult }

func (f *fakeWebSearcher) Search(_ context.Context, _ string, _ int) ([]port.WebResult, error) {
	return f.results, nil
}

func collectEvents(events *[]Event) func(Event) error {
	return func(event Event) error {
		*events = append(*events, event)
		return nil
	}
}

func TestServiceAgentTurnPersistsStepsAndRewritesHandles(t *testing.T) {
	model := &scriptedModel{responses: []*port.ChatResponse{
		{Content: "先检索。", ToolCalls: []port.LLMToolCall{toolCall("c1", "search_knowledge", "甲")}},
		{Content: "答案是甲 [c1]。", FinishReason: "stop"},
	}}
	rig := newServiceRig(t, model, settingsdom.Values{})
	var events []Event
	conversationBizID, result, err := rig.service.Chat(context.Background(), rig.kbbiz, "", "", "", "甲是什么", collectEvents(&events))
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "agent" || result.Rounds != 2 || result.Degraded {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Citations) != 1 || result.Citations[0].ChunkBizID != "chunk-1" {
		t.Fatalf("citations = %+v", result.Citations)
	}
	messages, err := rig.service.ListMessages(context.Background(), conversationBizID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages = %+v", err)
	}
	if messages[1].Content != "答案是甲 [1]。" {
		t.Fatalf("handle citation not rewritten: %q", messages[1].Content)
	}
	if len(messages[1].AgentSteps) != 2 || messages[1].AgentSteps[0].Handles["c1"] != "chunk-1" {
		t.Fatalf("steps = %+v", messages[1].AgentSteps)
	}
	var sawThought, sawReferences bool
	for _, event := range events {
		sawThought = sawThought || event.Type == EventThought
		sawReferences = sawReferences || event.Type == EventReferences
	}
	if !sawThought || !sawReferences {
		t.Fatalf("events = %+v", events)
	}
}

func TestServiceQuickProfileExposesOnlySearchKnowledge(t *testing.T) {
	var defsSeen []string
	model := &defsCapturingModel{defsSeen: &defsSeen}
	rig := newServiceRig(t, model, settingsdom.Values{WebSearchEnabled: true})
	var events []Event
	_, result, err := rig.service.Chat(context.Background(), rig.kbbiz, "", "", conversation.ModeQuick, "q", collectEvents(&events))
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != conversation.ModeQuick || result.Rounds > 2 {
		t.Fatalf("result = %+v", result)
	}
	if len(defsSeen) != 1 || defsSeen[0] != "search_knowledge" {
		t.Fatalf("quick tool defs = %v", defsSeen)
	}
}

// defsCapturingModel 记录第一轮收到的工具定义,然后直接作答。
type defsCapturingModel struct {
	defsSeen *[]string
}

func (m *defsCapturingModel) Complete(context.Context, []port.ChatMessage) (string, error) {
	return "兜底", nil
}

func (m *defsCapturingModel) Stream(_ context.Context, _ []port.ChatMessage, onToken func(string) error) error {
	return onToken("兜底")
}

func (m *defsCapturingModel) CompleteTools(_ context.Context, _ []port.ChatMessage, defs []port.ToolDef) (*port.ChatResponse, error) {
	if *m.defsSeen == nil {
		for _, def := range defs {
			*m.defsSeen = append(*m.defsSeen, def.Name)
		}
	}
	return &port.ChatResponse{Content: "直接作答", FinishReason: "stop"}, nil
}

func TestServiceAgentProfileInjectsWebSearchWhenEnabled(t *testing.T) {
	var defsSeen []string
	model := &defsCapturingModel{defsSeen: &defsSeen}
	rig := newServiceRig(t, model, settingsdom.Values{WebSearchEnabled: true, WebSearchMaxResults: 5})
	var events []Event
	if _, _, err := rig.service.Chat(context.Background(), rig.kbbiz, "", "", "", "q", collectEvents(&events)); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(defsSeen, ",")
	for _, want := range []string{"search_knowledge", "read_document", "list_documents", "web_search"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("agent tool defs missing %s: %v", want, defsSeen)
		}
	}
}

func TestServiceAgentProfileOmitsWebSearchWhenDisabled(t *testing.T) {
	var defsSeen []string
	model := &defsCapturingModel{defsSeen: &defsSeen}
	rig := newServiceRig(t, model, settingsdom.Values{WebSearchEnabled: false})
	var events []Event
	if _, _, err := rig.service.Chat(context.Background(), rig.kbbiz, "", "", "", "q", collectEvents(&events)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(defsSeen, ","), "web_search") {
		t.Fatalf("web_search must not be injected when disabled: %v", defsSeen)
	}
}

// toolsUnsupportedModel 模拟不支持工具调用的模型;Stream 走降级管线。
type toolsUnsupportedModel struct {
	streamed string
}

func (m *toolsUnsupportedModel) Complete(context.Context, []port.ChatMessage) (string, error) {
	return "兜底", nil
}

func (m *toolsUnsupportedModel) Stream(_ context.Context, _ []port.ChatMessage, onToken func(string) error) error {
	for _, token := range []string{"降级", "答案"} {
		if err := onToken(token); err != nil {
			return err
		}
	}
	m.streamed = "ok"
	return nil
}

func (m *toolsUnsupportedModel) CompleteTools(context.Context, []port.ChatMessage, []port.ToolDef) (*port.ChatResponse, error) {
	return nil, port.ErrToolsUnsupported
}

func TestServiceDegradesWhenModelLacksTools(t *testing.T) {
	rig := newServiceRig(t, &toolsUnsupportedModel{}, settingsdom.Values{})
	var events []Event
	conversationBizID, result, err := rig.service.Chat(context.Background(), rig.kbbiz, "", "", "", "q", collectEvents(&events))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Degraded || result.Mode != conversation.ModeQuick {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Citations) != 1 || result.Citations[0].SourceType != conversation.SourceTypeKBChunk {
		t.Fatalf("citations = %+v", result.Citations)
	}
	var sawReferences, sawTokens bool
	for _, event := range events {
		sawReferences = sawReferences || event.Type == EventReferences
		sawTokens = sawTokens || event.Type == EventToken
		if event.Type == EventToolCall {
			t.Fatal("degraded pipeline must not emit tool events")
		}
	}
	if !sawReferences || !sawTokens {
		t.Fatalf("events = %+v", events)
	}
	messages, _ := rig.service.ListMessages(context.Background(), conversationBizID)
	if len(messages) != 2 || messages[1].Content != "降级答案" || messages[1].AgentSteps != nil {
		t.Fatalf("messages = %+v", messages)
	}
}

// cancelOnToolsModel 在第一次工具调用请求时取消父 ctx,模拟用户在检索完成前停止。
type cancelOnToolsModel struct{ cancel context.CancelFunc }

func (m *cancelOnToolsModel) Complete(context.Context, []port.ChatMessage) (string, error) {
	return "兜底", nil
}

func (m *cancelOnToolsModel) Stream(_ context.Context, _ []port.ChatMessage, onToken func(string) error) error {
	return onToken("兜底")
}

func (m *cancelOnToolsModel) CompleteTools(ctx context.Context, _ []port.ChatMessage, _ []port.ToolDef) (*port.ChatResponse, error) {
	m.cancel()
	return nil, ctx.Err()
}

func TestServiceCancelPersistsStoppedNotice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rig := newServiceRig(t, &cancelOnToolsModel{cancel: cancel}, settingsdom.Values{})
	var events []Event
	conversationBizID, result, err := rig.service.Chat(ctx, rig.kbbiz, "", "", "", "q", collectEvents(&events))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated {
		t.Fatalf("result = %+v", result)
	}
	messages, _ := rig.service.ListMessages(context.Background(), conversationBizID)
	if len(messages) != 2 || messages[1].Content != stoppedNotice {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestServiceRejectsUnknownModelAndKB(t *testing.T) {
	rig := newServiceRig(t, &scriptedModel{}, settingsdom.Values{})
	var events []Event
	if _, _, err := rig.service.Chat(context.Background(), "missing", "", "", "", "q", collectEvents(&events)); !errors.Is(err, knowledgebase.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := rig.service.Chat(context.Background(), rig.kbbiz, "", "missing-model", "", "q", collectEvents(&events)); err == nil || !strings.Contains(err.Error(), "is not configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestServiceRejectsConversationFromAnotherKB(t *testing.T) {
	rig := newServiceRig(t, &scriptedModel{responses: []*port.ChatResponse{{Content: "a", FinishReason: "stop"}}}, settingsdom.Values{})
	var events []Event
	conversationBizID, _, err := rig.service.Chat(context.Background(), rig.kbbiz, "", "", "", "q", collectEvents(&events))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = rig.service.Chat(context.Background(), "other", conversationBizID, "", "", "q", collectEvents(&events))
	if !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("expected ownership error, got %v", err)
	}
}

func TestRewriteHandleCitations(t *testing.T) {
	handles := tools.NewHandles()
	handles.Assign("c", "chunk-1")
	handles.Assign("w", "https://example.com")
	answer := rewriteHandleCitations("见 [c1] 与 [w1],未知 [c9] 保留。", handles.Snapshot(), []conversation.Citation{
		{SourceType: conversation.SourceTypeKBChunk, ChunkBizID: "chunk-1"},
		{SourceType: conversation.SourceTypeWeb, URL: "https://example.com"},
	})
	if answer != "见 [1] 与 [2],未知 [c9] 保留。" {
		t.Fatalf("answer = %q", answer)
	}
}
