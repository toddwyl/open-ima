package db

import (
	"context"
	"errors"
	"testing"

	"open-ima/internal/domain/conversation"
)

func newConversationService(t *testing.T) (*conversation.ConversationService, context.Context) {
	t.Helper()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO knowledge_bases (kb_biz_id, name) VALUES ('kb1', '库')`); err != nil {
		t.Fatal(err)
	}
	return conversation.NewConversationService(NewConversationRepository(database)), context.Background()
}

func TestConversationEnsureAndOwnership(t *testing.T) {
	svc, ctx := newConversationService(t)
	c, err := svc.Ensure(ctx, "kb1", "", "你好,这是第一条问题", conversation.ModeQuick)
	if err != nil || c.ID <= 0 || c.BizID == "" || c.Title != "你好,这是第一条问题" {
		t.Fatalf("ensure = %+v err=%v", c, err)
	}
	if c.Mode != conversation.ModeQuick {
		t.Fatalf("mode = %q, want quick", c.Mode)
	}
	if _, err := svc.Ensure(ctx, "other", c.BizID, "q", ""); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("cross-kb error = %v", err)
	}
	again, err := svc.Ensure(ctx, "kb1", c.BizID, "q", "")
	if err != nil || again.ID != c.ID || again.BizID != c.BizID {
		t.Fatalf("re-ensure = %+v err=%v", again, err)
	}
}

func TestConversationMessagesOrderingAndCascade(t *testing.T) {
	svc, ctx := newConversationService(t)
	c, err := svc.Ensure(ctx, "kb1", "", "q", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Append(ctx, c.BizID, "user", "问题", nil, nil); err != nil {
		t.Fatal(err)
	}
	citations := []conversation.Citation{{MediaBizID: "d1", ChunkBizID: "c1", Snippet: "片段", Score: 0.5}}
	steps := []conversation.AgentStep{{
		Iteration: 1, Thought: "先查库",
		ToolCalls: []conversation.AgentToolCall{{ID: "call-1", Name: "search_knowledge", Success: true, Output: "结果", DurationMs: 12}},
		Handles:   map[string]string{"c1": "c1"},
	}}
	if err := svc.Append(ctx, c.BizID, "assistant", "回答", citations, steps); err != nil {
		t.Fatal(err)
	}
	messages, err := svc.ListMessages(ctx, c.BizID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages = %+v err=%v", messages, err)
	}
	if messages[0].Role != "user" || messages[1].Content != "回答" || len(messages[1].Citations) != 1 {
		t.Fatalf("messages = %+v", messages)
	}
	// 缺省 source_type 兼容旧数据,读出时归为 kb_chunk
	if messages[1].Citations[0].SourceType != conversation.SourceTypeKBChunk {
		t.Fatalf("source_type = %q", messages[1].Citations[0].SourceType)
	}
	if len(messages[1].AgentSteps) != 1 || messages[1].AgentSteps[0].ToolCalls[0].Name != "search_knowledge" ||
		messages[1].AgentSteps[0].Handles["c1"] != "c1" {
		t.Fatalf("agent steps = %+v", messages[1].AgentSteps)
	}
	if messages[0].AgentSteps != nil {
		t.Fatalf("user message should have no steps: %+v", messages[0].AgentSteps)
	}
	if messages[0].ID <= 0 || messages[1].ID <= messages[0].ID {
		t.Fatalf("message ids = %d, %d", messages[0].ID, messages[1].ID)
	}
	if messages[0].Citations == nil {
		t.Fatal("empty citations should normalize to []")
	}
	recent, err := svc.RecentMessages(ctx, c.BizID, 1)
	if err != nil || len(recent) != 1 || recent[0].Role != "assistant" {
		t.Fatalf("recent = %+v err=%v", recent, err)
	}
	empty, err := svc.ListMessages(ctx, "missing")
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty = %+v err=%v", empty, err)
	}
}

func TestConversationDeleteByKBCascadesMessages(t *testing.T) {
	svc, ctx := newConversationService(t)
	c, _ := svc.Ensure(ctx, "kb1", "", "q", "")
	_ = svc.Append(ctx, c.BizID, "user", "问题", nil, nil)
	if err := svc.DeleteByKB(ctx, "kb1"); err != nil {
		t.Fatal(err)
	}
	conversations, err := svc.ListByKB(ctx, "kb1")
	if err != nil || len(conversations) != 0 {
		t.Fatalf("conversations = %+v err=%v", conversations, err)
	}
	messages, err := svc.ListMessages(ctx, c.BizID)
	if err != nil || len(messages) != 0 {
		t.Fatalf("messages = %+v err=%v", messages, err)
	}
}
