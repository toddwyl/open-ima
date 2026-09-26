// Package db 打开并迁移 SQLite 元数据库。
package db

import (
	"context"
	"database/sql"
	_ "embed"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed migrations.sql
var migrations string

func Open(path string) (*sql.DB, error) {
	dsn := path
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared"
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// 单写者:避免 database is locked;也保证 :memory: 共享连接看到同一份数据
	d.SetMaxOpenConns(1)
	if path != ":memory:" {
		if _, err := d.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
			d.Close()
			return nil, err
		}
	}
	if _, err := d.ExecContext(context.Background(), migrations); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}
