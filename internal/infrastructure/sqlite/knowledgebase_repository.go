package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"open-ima/internal/domain/knowledgebase"
)

// KnowledgeBaseRepository 是 knowledgebase.Repository 的 SQLite 实现。
type KnowledgeBaseRepository struct{ db *sql.DB }

func NewKnowledgeBaseRepository(db *sql.DB) *KnowledgeBaseRepository {
	return &KnowledgeBaseRepository{db: db}
}

var _ knowledgebase.Repository = (*KnowledgeBaseRepository)(nil)

func (r *KnowledgeBaseRepository) Insert(ctx context.Context, kb *knowledgebase.KnowledgeBase) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO knowledge_bases (id, name, description) VALUES (?, ?, ?)`,
		kb.ID, kb.Name, kb.Description)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return knowledgebase.ErrNameTaken
	}
	return err
}

func (r *KnowledgeBaseRepository) Exists(ctx context.Context, id string) (bool, error) {
	var exists int
	if err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM knowledge_bases WHERE id = ?)`, id).Scan(&exists); err != nil {
		return false, err
	}
	return exists == 1, nil
}

func (r *KnowledgeBaseRepository) List(ctx context.Context) ([]knowledgebase.KnowledgeBase, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT k.id, k.name, k.description, k.created_at,
		       (SELECT COUNT(*) FROM documents d WHERE d.kb_id = k.id) AS doc_count
		FROM knowledge_bases k ORDER BY k.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	knowledgeBases := make([]knowledgebase.KnowledgeBase, 0)
	for rows.Next() {
		var kb knowledgebase.KnowledgeBase
		if err := rows.Scan(&kb.ID, &kb.Name, &kb.Description, &kb.CreatedAt, &kb.DocCount); err != nil {
			return nil, err
		}
		knowledgeBases = append(knowledgeBases, kb)
	}
	return knowledgeBases, rows.Err()
}

func (r *KnowledgeBaseRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM knowledge_bases WHERE id = ?`, id)
	return err
}
