package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"open-ima/internal/domain/conversation"
	"open-ima/internal/infrastructure/db/dao"
)

// ConversationRepository 是 conversation.ConversationRepository 的 SQL 实现:
// 行级操作委托给 DAO,此处负责实体映射、citations 编解码与错误翻译。
type ConversationRepository struct{ dao *dao.ConversationDAO }

func NewConversationRepository(db *sql.DB) *ConversationRepository {
	return &ConversationRepository{dao: dao.NewConversationDAO(db)}
}

var _ conversation.ConversationRepository = (*ConversationRepository)(nil)

func messageToEntity(row dao.MessageRow) (conversation.Message, error) {
	message := conversation.Message{
		BizID:             row.BizID,
		ConversationBizID: row.ConversationBizID,
		Role:              row.Role,
		Content:           row.Content,
		CreatedAt:         row.CreatedAt,
	}
	if err := json.Unmarshal([]byte(row.CitationsJSON), &message.Citations); err != nil {
		return conversation.Message{}, fmt.Errorf("decode citations: %w", err)
	}
	return message, nil
}

func messagesToEntities(rows []dao.MessageRow) ([]conversation.Message, error) {
	messages := make([]conversation.Message, 0, len(rows))
	for _, row := range rows {
		message, err := messageToEntity(row)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func (r *ConversationRepository) Get(ctx context.Context, id, kbBizID string) (*conversation.Conversation, error) {
	row, err := r.dao.Get(ctx, id, kbBizID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &conversation.Conversation{
		BizID:     row.BizID,
		KBBizID:   row.KBBizID,
		Title:     row.Title,
		CreatedAt: row.CreatedAt,
	}, nil
}

func (r *ConversationRepository) Insert(ctx context.Context, c *conversation.Conversation) error {
	return r.dao.Insert(ctx, dao.ConversationRow{
		BizID:   c.BizID,
		KBBizID: c.KBBizID,
		Title:   c.Title,
	})
}

func (r *ConversationRepository) ListByKB(ctx context.Context, kbBizID string) ([]conversation.Conversation, error) {
	rows, err := r.dao.ListByKB(ctx, kbBizID)
	if err != nil {
		return nil, err
	}
	conversations := make([]conversation.Conversation, 0, len(rows))
	for _, row := range rows {
		conversations = append(conversations, conversation.Conversation{
			BizID:     row.BizID,
			KBBizID:   row.KBBizID,
			Title:     row.Title,
			CreatedAt: row.CreatedAt,
		})
	}
	return conversations, nil
}

func (r *ConversationRepository) DeleteByKB(ctx context.Context, kbBizID string) error {
	return r.dao.DeleteByKB(ctx, kbBizID)
}

func (r *ConversationRepository) RecentMessages(ctx context.Context, conversationBizID string, limit int) ([]conversation.Message, error) {
	rows, err := r.dao.RecentMessages(ctx, conversationBizID, limit)
	if err != nil {
		return nil, err
	}
	return messagesToEntities(rows)
}

func (r *ConversationRepository) ListMessages(ctx context.Context, conversationBizID string) ([]conversation.Message, error) {
	rows, err := r.dao.ListMessages(ctx, conversationBizID)
	if err != nil {
		return nil, err
	}
	return messagesToEntities(rows)
}

func (r *ConversationRepository) AppendMessage(ctx context.Context, message *conversation.Message) error {
	if message.Citations == nil {
		message.Citations = []conversation.Citation{}
	}
	encoded, err := json.Marshal(message.Citations)
	if err != nil {
		return err
	}
	return r.dao.AppendMessage(ctx, dao.MessageRow{
		BizID:             message.BizID,
		ConversationBizID: message.ConversationBizID,
		Role:              message.Role,
		Content:           message.Content,
		CitationsJSON:     string(encoded),
	})
}
