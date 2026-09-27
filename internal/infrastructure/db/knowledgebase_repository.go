package db

import (
	"context"
	"database/sql"
	"strings"

	"open-ima/internal/domain/knowledgebase"
	"open-ima/internal/infrastructure/db/dao"
)

// KnowledgeBaseRepository 是 knowledgebase.KBRepository 的 SQL 实现:
// 行级操作委托给 DAO,此处负责实体映射与错误翻译。
type KnowledgeBaseRepository struct{ dao *dao.KnowledgeBaseDAO }

func NewKnowledgeBaseRepository(db *sql.DB) *KnowledgeBaseRepository {
	return &KnowledgeBaseRepository{dao: dao.NewKnowledgeBaseDAO(db)}
}

var _ knowledgebase.KBRepository = (*KnowledgeBaseRepository)(nil)

func (r *KnowledgeBaseRepository) Insert(ctx context.Context, kb *knowledgebase.KnowledgeBase) error {
	id, err := r.dao.Insert(ctx, dao.KnowledgeBaseRow{
		BizID:       kb.BizID,
		Name:        kb.Name,
		Description: kb.Description,
	})
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return knowledgebase.ErrNameTaken
	}
	if err != nil {
		return err
	}
	kb.ID = id
	return nil
}

func (r *KnowledgeBaseRepository) Exists(ctx context.Context, id string) (bool, error) {
	return r.dao.Exists(ctx, id)
}

func (r *KnowledgeBaseRepository) List(ctx context.Context) ([]knowledgebase.KnowledgeBase, error) {
	rows, err := r.dao.List(ctx)
	if err != nil {
		return nil, err
	}
	knowledgeBases := make([]knowledgebase.KnowledgeBase, 0, len(rows))
	for _, row := range rows {
		knowledgeBases = append(knowledgeBases, knowledgebase.KnowledgeBase{
			ID:          row.ID,
			BizID:       row.BizID,
			Name:        row.Name,
			Description: row.Description,
			MediaCount:    row.MediaCount,
			CreatedAt:   row.CreatedAt,
		})
	}
	return knowledgeBases, nil
}

func (r *KnowledgeBaseRepository) Delete(ctx context.Context, id string) error {
	return r.dao.Delete(ctx, id)
}
