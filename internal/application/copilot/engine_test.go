package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"open-ima/internal/application/port"
	"open-ima/internal/domain/conversation"
)

// scriptedModel 按预设脚本响应 CompleteTools 调用,并记录每次收到的对话线程。
type scriptedModel struct {
	responses []*port.ChatResponse
	mutate    func(resp *port.ChatResponse, round int) // 选中响应后按轮次改写
	err       error
	threads   [][]port.ChatMessage
	complete  *string // fallback Complete 的回答;nil 时为 "兜底答案"
}

func (m *scriptedModel) Complete(_ context.Context, _ []port.ChatMessage) (string, error) {
	if m.complete == nil {
		return "兜底答案", nil
	}
	return *m.complete, nil
}

func (m *scriptedModel) Stream(_ context.Context, _ []port.ChatMessage, onToken func(string) error) error {
	answer, _ := m.Complete(context.Background(), nil)
	return onToken(answer)
}

func (m *scriptedModel) CompleteTools(_ context.Context, messages []port.ChatMessage, _ []port.ToolDef) (*port.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.threads = append(m.threads, append([]port.ChatMessage{}, messages...))
	index := len(m.threads) - 1
	if index >= len(m.responses) {
		index = len(m.responses) - 1
	}
	resp := *m.responses[index]
	if m.mutate != nil {
		m.mutate(&resp, len(m.threads)-1)
	}
	return &resp, nil
}

// stubTools 是确定性 ToolSet:按工具名返回固定结果,输出带引用与句柄。
type stubTools struct {
	names   []string
	outputs map[string]string
	refs    map[string][]conversation.Citation
	handles map[string]map[string]string
}

func (s *stubTools) Defs() []port.ToolDef {
	defs := make([]port.ToolDef, len(s.names))
	for index, name := range s.names {
		defs[index] = port.ToolDef{Name: name}
	}
	return defs
}

func (s *stubTools) Execute(_ context.Context, name string, _ json.RawMessage) *port.ToolResult {
	return &port.ToolResult{
		Success: true,
		Output:  s.outputs[name],
		Data: map[string]any{
			"citations": s.refs[name],
			"handles":   s.handles[name],
		},
	}
}

func ptr(value string) *string { return &value }

func toolCall(id, name, query string) port.LLMToolCall {
	return port.LLMToolCall{ID: id, Name: name, Arguments: json.RawMessage(`{"query":"` + query + `"}`)}
}

func runEngine(t *testing.T, model *scriptedModel, tools ToolSet, profile Profile) (*Outcome, []Event, error) {
	t.Helper()
	var events []Event
	engine := NewEngine(model, Guards{MaxIterations: 20, MaxEmptyResponseRetries: 2,
		MaxRepeatedResponseRounds: 2, MaxConsecutiveLengthRounds: 3, LLMCallTimeout: 0, FinalAnswerChunkRunes: 8})
	// LLMCallTimeout 为 0 时由 NewEngine 归一默认值
	outcome, err := engine.Run(context.Background(), []port.ChatMessage{{Role: "user", Content: "问题"}}, tools, profile, func(event Event) error {
		events = append(events, event)
		return nil
	})
	return outcome, events, err
}

func eventTypes(events []Event) []EventType {
	types := make([]EventType, len(events))
	for index, event := range events {
		types[index] = event.Type
	}
	return types
}

func tokenText(events []Event) string {
	var text strings.Builder
	for _, event := range events {
		if event.Type == EventToken {
			text.WriteString(event.Content)
		}
	}
	return text.String()
}

func TestEngineKnowledgeAnswer(t *testing.T) {
	model := &scriptedModel{responses: []*port.ChatResponse{
		{ToolCalls: []port.LLMToolCall{toolCall("c1", "search_knowledge", "q")}},
		{Content: "答案是甲。[1]", FinishReason: "stop"},
	}}
	tools := &stubTools{names: []string{"search_knowledge"},
		outputs: map[string]string{"search_knowledge": "c1: 甲"},
		refs:    map[string][]conversation.Citation{"search_knowledge": {{SourceType: conversation.SourceTypeKBChunk, ChunkBizID: "c1", MediaBizID: "d1", Title: "文档"}}},
		handles: map[string]map[string]string{"search_knowledge": {"c1": "c1"}}}
	outcome, events, err := runEngine(t, model, tools, Profile{Name: "agent", SystemPrompt: "sys", MaxIterations: 20})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Answer != "答案是甲。[1]" || outcome.Rounds != 2 || outcome.Truncated {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(outcome.References) != 1 || outcome.References[0].SourceType != conversation.SourceTypeKBChunk {
		t.Fatalf("references = %+v", outcome.References)
	}
	if len(outcome.Steps) != 2 || outcome.Steps[0].ToolCalls[0].Name != "search_knowledge" || outcome.Steps[0].Handles["c1"] != "c1" {
		t.Fatalf("steps = %+v", outcome.Steps)
	}
	// 事件序列:tool_call → tool_result → references → token*
	want := []EventType{EventToolCall, EventToolResult, EventReferences, EventToken}
	got := eventTypes(events)
	if len(got) != len(want) {
		t.Fatalf("events = %v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("events = %v", got)
		}
	}
	if tokenText(events) != "答案是甲。[1]" {
		t.Fatalf("tokens = %q", tokenText(events))
	}
	// 第二轮线程:system + user + assistant(tool_calls) + tool
	thread := model.threads[1]
	if len(thread) != 4 || thread[0].Role != "system" || thread[2].Role != "assistant" || thread[3].Role != "tool" || thread[3].ToolCallID != "c1" {
		t.Fatalf("thread = %+v", thread)
	}
}

// 回归:最终答案里的来源句柄必须在 emitAnswer 阶段改写成 references 序号,
// 保证流式 token、持久化答案与引用区一致(曾出现流式内容残留 [c2] 的缺陷)。
func TestEngineAnswerHandlesRewrittenBeforeStreaming(t *testing.T) {
	model := &scriptedModel{responses: []*port.ChatResponse{
		{ToolCalls: []port.LLMToolCall{toolCall("c1", "search_knowledge", "q")}},
		{Content: "代号是 ORCHID-7429 [c1]。", FinishReason: "stop"},
	}}
	tools := &stubTools{names: []string{"search_knowledge"},
		outputs: map[string]string{"search_knowledge": "c1: 代号 ORCHID-7429"},
		refs:    map[string][]conversation.Citation{"search_knowledge": {{SourceType: conversation.SourceTypeKBChunk, ChunkBizID: "chunk-9", MediaBizID: "d1", Title: "文档"}}},
		handles: map[string]map[string]string{"search_knowledge": {"c1": "chunk-9"}}}
	outcome, events, err := runEngine(t, model, tools, Profile{Name: "agent", MaxIterations: 20})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Answer != "代号是 ORCHID-7429 [1]。" {
		t.Fatalf("answer = %q", outcome.Answer)
	}
	if tokenText(events) != "代号是 ORCHID-7429 [1]。" {
		t.Fatalf("tokens = %q", tokenText(events))
	}
}

func TestEnginePreambleBecomesThought(t *testing.T) {
	model := &scriptedModel{responses: []*port.ChatResponse{
		{Content: "让我查一下。", ToolCalls: []port.LLMToolCall{toolCall("c1", "search_knowledge", "q")}},
		{Content: "最终答案", FinishReason: "stop"},
	}}
	outcome, events, err := runEngine(t, model, &stubTools{names: []string{"search_knowledge"}}, Profile{Name: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Answer != "最终答案" {
		t.Fatalf("answer must not contain preamble: %q", outcome.Answer)
	}
	if len(events) == 0 || events[0].Type != EventThought || events[0].Content != "让我查一下。" || events[0].Round != 1 {
		t.Fatalf("events = %+v", events)
	}
}

// 回归:流入答案的轮次(自然停止/length 截断/卡死收尾)不得把答案文本留在 step.Thought,
// 否则历史消息回放时步骤树会把答案原文再展示一遍,与下方富文本答案重复。
func TestEngineAnswerRoundThoughtCleared(t *testing.T) {
	model := &scriptedModel{responses: []*port.ChatResponse{
		{Content: "让我查一下。", ToolCalls: []port.LLMToolCall{toolCall("c1", "search_knowledge", "q")}},
		{Content: "最终答案", FinishReason: "stop"},
	}}
	outcome, _, err := runEngine(t, model, &stubTools{names: []string{"search_knowledge"}}, Profile{Name: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Steps) != 2 {
		t.Fatalf("steps = %+v", outcome.Steps)
	}
	if outcome.Steps[0].Thought != "让我查一下。" {
		t.Fatalf("preamble thought = %q", outcome.Steps[0].Thought)
	}
	if outcome.Steps[1].Thought != "" {
		t.Fatalf("answer round thought must be empty, got %q", outcome.Steps[1].Thought)
	}
}

func TestEngineLengthRoundThoughtCleared(t *testing.T) {
	model := &scriptedModel{responses: []*port.ChatResponse{
		{Content: "第一段", FinishReason: "length"},
		{Content: "第二段", FinishReason: "stop"},
	}}
	outcome, _, err := runEngine(t, model, &stubTools{names: []string{"search_knowledge"}}, Profile{Name: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Answer != "第一段第二段" {
		t.Fatalf("answer = %q", outcome.Answer)
	}
	for index, step := range outcome.Steps {
		if step.Thought != "" {
			t.Fatalf("step %d thought must be empty, got %q", index, step.Thought)
		}
	}
}

func TestEngineDirectAnswerWithoutTools(t *testing.T) {
	model := &scriptedModel{responses: []*port.ChatResponse{{Content: "无需检索,直接作答。", FinishReason: "stop"}}}
	outcome, events, err := runEngine(t, model, &stubTools{names: []string{"search_knowledge"}}, Profile{Name: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Answer != "无需检索,直接作答。" || outcome.Rounds != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
	for _, event := range events {
		if event.Type == EventToolCall || event.Type == EventToolResult {
			t.Fatalf("unexpected tool event: %+v", event)
		}
	}
}

func TestEngineMultiHopResearch(t *testing.T) {
	model := &scriptedModel{responses: []*port.ChatResponse{
		{ToolCalls: []port.LLMToolCall{toolCall("c1", "search_knowledge", "一期")}},
		{ToolCalls: []port.LLMToolCall{toolCall("c2", "web_search", "二期")}},
		{Content: "综合答案", FinishReason: "stop"},
	}}
	tools := &stubTools{names: []string{"search_knowledge", "web_search"},
		refs: map[string][]conversation.Citation{
			"search_knowledge": {{SourceType: conversation.SourceTypeKBChunk, ChunkBizID: "c1"}},
			"web_search":       {{SourceType: conversation.SourceTypeWeb, URL: "https://example.com", Title: "网页"}},
		}}
	outcome, _, err := runEngine(t, model, tools, Profile{Name: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Rounds != 3 || len(outcome.References) != 2 {
		t.Fatalf("outcome = %+v", outcome)
	}
	if outcome.References[0].SourceType != conversation.SourceTypeKBChunk || outcome.References[1].SourceType != conversation.SourceTypeWeb {
		t.Fatalf("references = %+v", outcome.References)
	}
}

func TestEngineStuckGuard(t *testing.T) {
	stuck := &port.ChatResponse{Content: "还在想", ToolCalls: []port.LLMToolCall{toolCall("c1", "search_knowledge", "same")}}
	model := &scriptedModel{responses: []*port.ChatResponse{stuck, stuck, stuck, stuck}}
	outcome, _, err := runEngine(t, model, &stubTools{names: []string{"search_knowledge"}}, Profile{Name: "agent", MaxIterations: 20})
	if err != nil {
		t.Fatal(err)
	}
	// 第 2 轮与第 1 轮签名相同(1 次重复),第 3 轮再次相同(2 次重复,达到阈值)→ 停止
	if outcome.Rounds != 3 {
		t.Fatalf("rounds = %d, want 3", outcome.Rounds)
	}
	if outcome.Answer != "还在想" {
		t.Fatalf("answer = %q, want 末轮文本", outcome.Answer)
	}
}

func TestEngineMaxIterationsFallback(t *testing.T) {
	model := &scriptedModel{
		responses: []*port.ChatResponse{{ToolCalls: []port.LLMToolCall{toolCall("c1", "search_knowledge", "q")}}},
		mutate: func(resp *port.ChatResponse, round int) {
			resp.ToolCalls[0].Arguments = json.RawMessage(`{"query":"q` + string(rune('a'+round)) + `"}`)
		},
		complete: ptr("基于已有信息的答案"),
	}
	outcome, _, err := runEngine(t, model, &stubTools{names: []string{"search_knowledge"}}, Profile{Name: "agent", MaxIterations: 3})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Rounds != 3 || !outcome.Truncated || outcome.Answer != "基于已有信息的答案" {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestEngineEmptyResponseRetries(t *testing.T) {
	model := &scriptedModel{responses: []*port.ChatResponse{
		{Content: "", FinishReason: "stop"},
		{Content: "补上的答案", FinishReason: "stop"},
	}}
	outcome, _, err := runEngine(t, model, &stubTools{names: []string{"search_knowledge"}}, Profile{Name: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Answer != "补上的答案" {
		t.Fatalf("answer = %q", outcome.Answer)
	}
	thread := model.threads[1]
	if thread[len(thread)-1].Content != "请给出完整答案。" {
		t.Fatalf("retry prompt missing: %+v", thread)
	}
}

func TestEngineEmptyResponseFallback(t *testing.T) {
	model := &scriptedModel{responses: []*port.ChatResponse{
		{Content: "", FinishReason: "stop"},
		{Content: "", FinishReason: "stop"},
		{Content: "", FinishReason: "stop"},
	}, complete: ptr("")}
	outcome, _, err := runEngine(t, model, &stubTools{names: []string{"search_knowledge"}}, Profile{Name: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Truncated || !strings.Contains(outcome.Answer, "未能在限定步骤内") {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestEngineConsecutiveLengthTruncation(t *testing.T) {
	model := &scriptedModel{responses: []*port.ChatResponse{
		{Content: "第一段", FinishReason: "length"},
		{Content: "第二段", FinishReason: "length"},
		{Content: "第三段", FinishReason: "length"},
	}}
	outcome, _, err := runEngine(t, model, &stubTools{names: []string{"search_knowledge"}}, Profile{Name: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Truncated || outcome.Answer != "第一段第二段第三段" {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestEngineCancelWithToolResultsSynthesizes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	model := &scriptedModel{responses: []*port.ChatResponse{
		{ToolCalls: []port.LLMToolCall{toolCall("c1", "search_knowledge", "q")}},
	}, complete: ptr("取消时的合成答案")}
	tools := &stubTools{names: []string{"search_knowledge"}, outputs: map[string]string{"search_knowledge": "结果"}}
	cancelAfterFirst := &cancelModel{inner: model, cancel: cancel}
	engine := NewEngine(cancelAfterFirst, Guards{})
	var events []Event
	outcome, err := engine.Run(ctx, []port.ChatMessage{{Role: "user", Content: "q"}}, tools, Profile{Name: "agent", MaxIterations: 5}, func(event Event) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Answer != "取消时的合成答案" {
		t.Fatalf("answer = %q", outcome.Answer)
	}
}

// cancelModel 在第二次 CompleteTools 前取消父 ctx,模拟用户在工具结果落地后停止。
type cancelModel struct {
	inner  *scriptedModel
	cancel context.CancelFunc
	calls  int
}

func (m *cancelModel) Complete(ctx context.Context, messages []port.ChatMessage) (string, error) {
	return m.inner.Complete(ctx, messages)
}

func (m *cancelModel) Stream(ctx context.Context, messages []port.ChatMessage, onToken func(string) error) error {
	return m.inner.Stream(ctx, messages, onToken)
}

func (m *cancelModel) CompleteTools(ctx context.Context, messages []port.ChatMessage, defs []port.ToolDef) (*port.ChatResponse, error) {
	m.calls++
	if m.calls >= 2 {
		m.cancel()
		return nil, ctx.Err()
	}
	return m.inner.CompleteTools(ctx, messages, defs)
}

func TestEngineCancelWithoutResultsPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine := NewEngine(&scriptedModel{}, Guards{})
	_, err := engine.Run(ctx, []port.ChatMessage{{Role: "user", Content: "q"}}, &stubTools{names: []string{"search_knowledge"}}, Profile{Name: "agent"}, func(Event) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestEngineModelErrorPropagates(t *testing.T) {
	want := errors.New("model boom")
	model := &scriptedModel{err: want}
	engine := NewEngine(model, Guards{})
	_, err := engine.Run(context.Background(), []port.ChatMessage{{Role: "user", Content: "q"}}, &stubTools{names: []string{"search_knowledge"}}, Profile{Name: "agent"}, func(Event) error { return nil })
	if !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}
