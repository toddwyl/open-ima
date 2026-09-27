package dao

import (
	"context"
	"database/sql"
)

// SettingsDAO 封装 app_settings 表的行级操作。
type SettingsDAO struct{ db *sql.DB }

func NewSettingsDAO(db *sql.DB) *SettingsDAO { return &SettingsDAO{db: db} }

func (d *SettingsDAO) Load(ctx context.Context) (map[string]string, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT key, value FROM app_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, rows.Err()
}

// Save 在一个事务里逐键 upsert。
func (d *SettingsDAO) Save(ctx context.Context, values map[string]string) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, `INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}
