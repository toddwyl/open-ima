package dao

import (
	"context"
	"database/sql"
	"time"
)

// ConversationRow 是 conversations 表的一行。
type ConversationRow struct {
	ID        int64
	BizID     string
	KBBizID   string
	Title     string
	Mode      string
	CreatedAt time.Time
}

// MessageRow 是 messages 表的一行;CitationsJSON 为引用列表的 JSON 编码,
// AgentStepsJSON 为 agent 步骤轨迹的 JSON 编码(旧数据为空),编解码由上层 Repository 负责。
type MessageRow struct {
	ID                int64
	BizID             string
	ConversationBizID string
	Role              string
	Content           string
	CitationsJSON     string
	AgentStepsJSON    sql.NullString
	CreatedAt         time.Time
}

const messageColumns = `id, message_biz_id, conversation_biz_id, role, content, citations, agent_steps, created_at`

func scanMessage(row scanner) (MessageRow, error) {
	var message MessageRow
	err := row.Scan(
		&message.ID, &message.BizID, &message.ConversationBizID, &message.Role, &message.Content,
		&message.CitationsJSON, &message.AgentStepsJSON, &message.CreatedAt,
	)
	return message, err
}

// ConversationDAO 封装 conversations 与 messages 表的行级操作。
type ConversationDAO struct{ db *sql.DB }

func NewConversationDAO(db *sql.DB) *ConversationDAO { return &ConversationDAO{db: db} }

// Get 未命中返回 sql.ErrNoRows。
func (d *ConversationDAO) Get(ctx context.Context, id, kbBizID string) (*ConversationRow, error) {
	var row ConversationRow
	err := d.db.QueryRowContext(ctx,
		`SELECT id, conversation_biz_id, kb_biz_id, title, mode, created_at FROM conversations WHERE conversation_biz_id = ? AND kb_biz_id = ?`,
		id, kbBizID).Scan(&row.ID, &row.BizID, &row.KBBizID, &row.Title, &row.Mode, &row.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (d *ConversationDAO) Insert(ctx context.Context, row ConversationRow) (int64, error) {
	result, err := d.db.ExecContext(ctx,
		`INSERT INTO conversations (conversation_biz_id, kb_biz_id, title, mode) VALUES (?, ?, ?, ?)`, row.BizID, row.KBBizID, row.Title, row.Mode)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (d *ConversationDAO) ListByKB(ctx context.Context, kbBizID string) ([]ConversationRow, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, conversation_biz_id, kb_biz_id, title, mode, created_at FROM conversations WHERE kb_biz_id = ? ORDER BY id DESC`, kbBizID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	conversations := make([]ConversationRow, 0)
	for rows.Next() {
		var row ConversationRow
		if err := rows.Scan(&row.ID, &row.BizID, &row.KBBizID, &row.Title, &row.CreatedAt); err != nil {
			return nil, err
		}
		conversations = append(conversations, row)
	}
	return conversations, rows.Err()
}

// DeleteByKB 在一个事务里删除知识库下全部会话及其消息。
func (d *ConversationDAO) DeleteByKB(ctx context.Context, kbBizID string) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM messages WHERE conversation_biz_id IN (SELECT conversation_biz_id FROM conversations WHERE kb_biz_id = ?)`, kbBizID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM conversations WHERE kb_biz_id = ?`, kbBizID); err != nil {
		return err
	}
	return tx.Commit()
}

// RecentMessages 按时间正序返回最近 limit 条消息。
func (d *ConversationDAO) RecentMessages(ctx context.Context, conversationBizID string, limit int) ([]MessageRow, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT `+messageColumns+` FROM messages WHERE conversation_biz_id = ? ORDER BY id DESC LIMIT ?`,
		conversationBizID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []MessageRow
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

func (d *ConversationDAO) ListMessages(ctx context.Context, conversationBizID string) ([]MessageRow, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT `+messageColumns+` FROM messages WHERE conversation_biz_id = ? ORDER BY id`,
		conversationBizID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]MessageRow, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (d *ConversationDAO) AppendMessage(ctx context.Context, row MessageRow) (int64, error) {
	result, err := d.db.ExecContext(ctx,
		`INSERT INTO messages (message_biz_id, conversation_biz_id, role, content, citations, agent_steps) VALUES (?, ?, ?, ?, ?, ?)`,
		row.BizID, row.ConversationBizID, row.Role, row.Content, row.CitationsJSON, row.AgentStepsJSON)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
