package dao

import (
	"context"
	"database/sql"
	"time"
)

// KnowledgeBaseRow 是 knowledge_bases 表的一行;DocCount 来自关联文档数统计。
type KnowledgeBaseRow struct {
	BizID       string
	Name        string
	Description string
	DocCount    int
	CreatedAt   time.Time
}

// KnowledgeBaseDAO 封装 knowledge_bases 表的行级操作。
type KnowledgeBaseDAO struct{ db *sql.DB }

func NewKnowledgeBaseDAO(db *sql.DB) *KnowledgeBaseDAO { return &KnowledgeBaseDAO{db: db} }

func (d *KnowledgeBaseDAO) Insert(ctx context.Context, row KnowledgeBaseRow) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO knowledge_bases (kb_biz_id, name, description) VALUES (?, ?, ?)`,
		row.BizID, row.Name, row.Description)
	return err
}

func (d *KnowledgeBaseDAO) Exists(ctx context.Context, id string) (bool, error) {
	var exists int
	if err := d.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM knowledge_bases WHERE kb_biz_id = ?)`, id).Scan(&exists); err != nil {
		return false, err
	}
	return exists == 1, nil
}

func (d *KnowledgeBaseDAO) List(ctx context.Context) ([]KnowledgeBaseRow, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT k.kb_biz_id, k.name, k.description, k.created_at,
		       (SELECT COUNT(*) FROM documents d WHERE d.kb_biz_id = k.kb_biz_id) AS doc_count
		FROM knowledge_bases k ORDER BY k.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	knowledgeBases := make([]KnowledgeBaseRow, 0)
	for rows.Next() {
		var row KnowledgeBaseRow
		if err := rows.Scan(&row.BizID, &row.Name, &row.Description, &row.CreatedAt, &row.DocCount); err != nil {
			return nil, err
		}
		knowledgeBases = append(knowledgeBases, row)
	}
	return knowledgeBases, rows.Err()
}

func (d *KnowledgeBaseDAO) Delete(ctx context.Context, id string) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM knowledge_bases WHERE kb_biz_id = ?`, id)
	return err
}
