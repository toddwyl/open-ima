package db

import (
	"context"
	"database/sql"

	"open-ima/internal/domain/settings"
	"open-ima/internal/infrastructure/db/dao"
)

// SettingsRepository 是 settings.SettingsRepository 的 SQL 实现:
// 行级操作委托给 DAO。
type SettingsRepository struct{ dao *dao.SettingsDAO }

func NewSettingsRepository(db *sql.DB) *SettingsRepository {
	return &SettingsRepository{dao: dao.NewSettingsDAO(db)}
}

var _ settings.SettingsRepository = (*SettingsRepository)(nil)

func (r *SettingsRepository) Load(ctx context.Context) (map[string]string, error) {
	return r.dao.Load(ctx)
}

func (r *SettingsRepository) Save(ctx context.Context, values map[string]string) error {
	return r.dao.Save(ctx, values)
}
