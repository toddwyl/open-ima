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
		ID:             row.ID,
		ConversationID: row.ConversationID,
		Role:           row.Role,
		Content:        row.Content,
		CreatedAt:      row.CreatedAt,
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

func (r *ConversationRepository) Get(ctx context.Context, id, kbID string) (*conversation.Conversation, error) {
	row, err := r.dao.Get(ctx, id, kbID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &conversation.Conversation{
		ID:        row.ID,
		KBID:      row.KBID,
		Title:     row.Title,
		CreatedAt: row.CreatedAt,
	}, nil
}

func (r *ConversationRepository) Insert(ctx context.Context, c *conversation.Conversation) error {
	return r.dao.Insert(ctx, dao.ConversationRow{
		ID:    c.ID,
		KBID:  c.KBID,
		Title: c.Title,
	})
}

func (r *ConversationRepository) ListByKB(ctx context.Context, kbID string) ([]conversation.Conversation, error) {
	rows, err := r.dao.ListByKB(ctx, kbID)
	if err != nil {
		return nil, err
	}
	conversations := make([]conversation.Conversation, 0, len(rows))
	for _, row := range rows {
		conversations = append(conversations, conversation.Conversation{
			ID:        row.ID,
			KBID:      row.KBID,
			Title:     row.Title,
			CreatedAt: row.CreatedAt,
		})
	}
	return conversations, nil
}

func (r *ConversationRepository) DeleteByKB(ctx context.Context, kbID string) error {
	return r.dao.DeleteByKB(ctx, kbID)
}

func (r *ConversationRepository) RecentMessages(ctx context.Context, conversationID string, limit int) ([]conversation.Message, error) {
	rows, err := r.dao.RecentMessages(ctx, conversationID, limit)
	if err != nil {
		return nil, err
	}
	return messagesToEntities(rows)
}

func (r *ConversationRepository) ListMessages(ctx context.Context, conversationID string) ([]conversation.Message, error) {
	rows, err := r.dao.ListMessages(ctx, conversationID)
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
		ID:             message.ID,
		ConversationID: message.ConversationID,
		Role:           message.Role,
		Content:        message.Content,
		CitationsJSON:  string(encoded),
	})
}
