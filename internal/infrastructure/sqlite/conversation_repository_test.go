package sqlite

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
	if _, err := database.Exec(`INSERT INTO knowledge_bases (id, name) VALUES ('kb1', '库')`); err != nil {
		t.Fatal(err)
	}
	return conversation.NewConversationService(NewConversationRepository(database)), context.Background()
}

func TestConversationEnsureAndOwnership(t *testing.T) {
	svc, ctx := newConversationService(t)
	c, err := svc.Ensure(ctx, "kb1", "", "你好,这是第一条问题")
	if err != nil || c.ID == "" || c.Title != "你好,这是第一条问题" {
		t.Fatalf("ensure = %+v err=%v", c, err)
	}
	if _, err := svc.Ensure(ctx, "other", c.ID, "q"); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("cross-kb error = %v", err)
	}
	again, err := svc.Ensure(ctx, "kb1", c.ID, "q")
	if err != nil || again.ID != c.ID {
		t.Fatalf("re-ensure = %+v err=%v", again, err)
	}
}

func TestConversationMessagesOrderingAndCascade(t *testing.T) {
	svc, ctx := newConversationService(t)
	c, err := svc.Ensure(ctx, "kb1", "", "q")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Append(ctx, c.ID, "user", "问题", nil); err != nil {
		t.Fatal(err)
	}
	citations := []conversation.Citation{{DocumentID: "d1", ChunkID: "c1", Snippet: "片段", Score: 0.5}}
	if err := svc.Append(ctx, c.ID, "assistant", "回答", citations); err != nil {
		t.Fatal(err)
	}
	messages, err := svc.ListMessages(ctx, c.ID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages = %+v err=%v", messages, err)
	}
	if messages[0].Role != "user" || messages[1].Content != "回答" || len(messages[1].Citations) != 1 {
		t.Fatalf("messages = %+v", messages)
	}
	if messages[0].Citations == nil {
		t.Fatal("empty citations should normalize to []")
	}
	recent, err := svc.RecentMessages(ctx, c.ID, 1)
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
	c, _ := svc.Ensure(ctx, "kb1", "", "q")
	_ = svc.Append(ctx, c.ID, "user", "问题", nil)
	if err := svc.DeleteByKB(ctx, "kb1"); err != nil {
		t.Fatal(err)
	}
	conversations, err := svc.ListByKB(ctx, "kb1")
	if err != nil || len(conversations) != 0 {
		t.Fatalf("conversations = %+v err=%v", conversations, err)
	}
	messages, err := svc.ListMessages(ctx, c.ID)
	if err != nil || len(messages) != 0 {
		t.Fatalf("messages = %+v err=%v", messages, err)
	}
}
