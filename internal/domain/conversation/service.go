package conversation

import (
	"context"
	"time"

	"open-ima/internal/pkg/idgen"
)

// Service 承载会话创建与消息排序规则。
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service { return &Service{repo: repo} }

// Ensure 返回既有会话;conversationID 为空时以首条问题摘要创建新会话。
// 调用方需先确认知识库存在。
func (s *Service) Ensure(ctx context.Context, kbID, conversationID, query string) (*Conversation, error) {
	if conversationID != "" {
		return s.repo.Get(ctx, conversationID, kbID)
	}
	conversation := &Conversation{
		ID: idgen.New(), KBID: kbID, Title: truncateRunes(query, 80), CreatedAt: time.Now(),
	}
	if err := s.repo.Insert(ctx, conversation); err != nil {
		return nil, err
	}
	return conversation, nil
}

func (s *Service) ListByKB(ctx context.Context, kbID string) ([]Conversation, error) {
	return s.repo.ListByKB(ctx, kbID)
}

func (s *Service) RecentMessages(ctx context.Context, conversationID string, limit int) ([]Message, error) {
	return s.repo.RecentMessages(ctx, conversationID, limit)
}

func (s *Service) ListMessages(ctx context.Context, conversationID string) ([]Message, error) {
	return s.repo.ListMessages(ctx, conversationID)
}

// DeleteByKB 删除知识库下全部会话及其消息,供知识库级联删除编排调用。
func (s *Service) DeleteByKB(ctx context.Context, kbID string) error {
	return s.repo.DeleteByKB(ctx, kbID)
}

// Append 追加一条消息;引用为空时归一化为空数组而非 null。
func (s *Service) Append(ctx context.Context, conversationID, role, content string, citations []Citation) error {
	if citations == nil {
		citations = []Citation{}
	}
	return s.repo.AppendMessage(ctx, &Message{
		ID: idgen.New(), ConversationID: conversationID, Role: role,
		Content: content, Citations: citations, CreatedAt: time.Now(),
	})
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
