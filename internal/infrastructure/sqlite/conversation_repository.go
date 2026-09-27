package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"open-ima/internal/domain/conversation"
)

// ConversationRepository 是 conversation.Repository 的 SQLite 实现。
type ConversationRepository struct{ db *sql.DB }

func NewConversationRepository(db *sql.DB) *ConversationRepository {
	return &ConversationRepository{db: db}
}

var _ conversation.Repository = (*ConversationRepository)(nil)

func (r *ConversationRepository) Get(ctx context.Context, id, kbID string) (*conversation.Conversation, error) {
	var c conversation.Conversation
	err := r.db.QueryRowContext(ctx,
		`SELECT id, kb_id, title, created_at FROM conversations WHERE id = ? AND kb_id = ?`,
		id, kbID).Scan(&c.ID, &c.KBID, &c.Title, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *ConversationRepository) Insert(ctx context.Context, c *conversation.Conversation) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO conversations (id, kb_id, title) VALUES (?, ?, ?)`, c.ID, c.KBID, c.Title)
	return err
}

func (r *ConversationRepository) ListByKB(ctx context.Context, kbID string) ([]conversation.Conversation, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, kb_id, title, created_at FROM conversations WHERE kb_id = ? ORDER BY created_at DESC`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	conversations := make([]conversation.Conversation, 0)
	for rows.Next() {
		var c conversation.Conversation
		if err := rows.Scan(&c.ID, &c.KBID, &c.Title, &c.CreatedAt); err != nil {
			return nil, err
		}
		conversations = append(conversations, c)
	}
	return conversations, rows.Err()
}

func (r *ConversationRepository) DeleteByKB(ctx context.Context, kbID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM messages WHERE conversation_id IN (SELECT id FROM conversations WHERE kb_id = ?)`, kbID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM conversations WHERE kb_id = ?`, kbID); err != nil {
		return err
	}
	return tx.Commit()
}

type messageScanner interface{ Scan(dest ...any) error }

func scanMessage(row messageScanner) (conversation.Message, error) {
	var message conversation.Message
	var citationsJSON string
	err := row.Scan(
		&message.ID, &message.ConversationID, &message.Role, &message.Content,
		&citationsJSON, &message.CreatedAt,
	)
	if err != nil {
		return conversation.Message{}, err
	}
	if err := json.Unmarshal([]byte(citationsJSON), &message.Citations); err != nil {
		return conversation.Message{}, fmt.Errorf("decode citations: %w", err)
	}
	return message, nil
}

const messageColumns = `id, conversation_id, role, content, citations, created_at`

func (r *ConversationRepository) RecentMessages(ctx context.Context, conversationID string, limit int) ([]conversation.Message, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+messageColumns+` FROM messages WHERE conversation_id = ? ORDER BY created_at DESC, rowid DESC LIMIT ?`,
		conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []conversation.Message
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	return messages, nil
}

func (r *ConversationRepository) ListMessages(ctx context.Context, conversationID string) ([]conversation.Message, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+messageColumns+` FROM messages WHERE conversation_id = ? ORDER BY created_at, rowid`,
		conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]conversation.Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (r *ConversationRepository) AppendMessage(ctx context.Context, message *conversation.Message) error {
	if message.Citations == nil {
		message.Citations = []conversation.Citation{}
	}
	encoded, err := json.Marshal(message.Citations)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO messages (id, conversation_id, role, content, citations) VALUES (?, ?, ?, ?, ?)`,
		message.ID, message.ConversationID, message.Role, message.Content, string(encoded))
	return err
}
