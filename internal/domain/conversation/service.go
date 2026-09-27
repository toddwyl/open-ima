package conversation

import (
	"context"
	"time"

	"open-ima/internal/pkg/idgen"
)

// Service 承载会话创建与消息排序规则。
type ConversationService struct {
	repo ConversationRepository
}

func NewConversationService(repo ConversationRepository) *ConversationService {
	return &ConversationService{repo: repo}
}

// Ensure 返回既有会话;conversationBizID 为空时以首条问题摘要创建新会话。
// mode 仅在创建新会话时生效(quick|agent,空串归为 agent);既有会话保持其既有模式。
// 调用方需先确认知识库存在。
func (s *ConversationService) Ensure(ctx context.Context, kbBizID, conversationBizID, query, mode string) (*Conversation, error) {
	if conversationBizID != "" {
		return s.repo.Get(ctx, conversationBizID, kbBizID)
	}
	if mode != ModeQuick {
		mode = ModeAgent
	}
	conversation := &Conversation{
		BizID: idgen.New(), KBBizID: kbBizID, Title: truncateRunes(query, 80), Mode: mode, CreatedAt: time.Now(),
	}
	if err := s.repo.Insert(ctx, conversation); err != nil {
		return nil, err
	}
	return conversation, nil
}

func (s *ConversationService) ListByKB(ctx context.Context, kbBizID string) ([]Conversation, error) {
	return s.repo.ListByKB(ctx, kbBizID)
}

func (s *ConversationService) RecentMessages(ctx context.Context, conversationBizID string, limit int) ([]Message, error) {
	return s.repo.RecentMessages(ctx, conversationBizID, limit)
}

func (s *ConversationService) ListMessages(ctx context.Context, conversationBizID string) ([]Message, error) {
	return s.repo.ListMessages(ctx, conversationBizID)
}

// DeleteByKB 删除知识库下全部会话及其消息,供知识库级联删除编排调用。
func (s *ConversationService) DeleteByKB(ctx context.Context, kbBizID string) error {
	return s.repo.DeleteByKB(ctx, kbBizID)
}

// Append 追加一条消息;引用为空时归一化为空数组而非 null;steps 为 agent 步骤轨迹(可空)。
func (s *ConversationService) Append(ctx context.Context, conversationBizID, role, content string, citations []Citation, steps []AgentStep) error {
	if citations == nil {
		citations = []Citation{}
	}
	return s.repo.AppendMessage(ctx, &Message{
		BizID: idgen.New(), ConversationBizID: conversationBizID, Role: role,
		Content: content, Citations: citations, AgentSteps: steps, CreatedAt: time.Now(),
	})
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
