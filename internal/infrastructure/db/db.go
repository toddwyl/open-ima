// Package db 打开并迁移 SQLite 元数据库。
package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed migrations.sql
var migrations string

// schemaVersion 是当前 migrations.sql 的版本,写入 PRAGMA user_version。
// v2/v3 库通过增量 ALTER TABLE 升级;更老的版本直接报错,
// 由用户删除 db 文件重建。
const schemaVersion = 4

// upgradeV2ToV3 是 v2 → v3 的增量迁移:会话加模式列,消息加步骤轨迹列。
const upgradeV2ToV3 = `
ALTER TABLE conversations ADD COLUMN mode TEXT NOT NULL DEFAULT 'agent';
ALTER TABLE messages ADD COLUMN agent_steps TEXT;
`

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
	if err := migrate(d, path); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func migrate(d *sql.DB, path string) error {
	var version int
	if err := d.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	// 守卫:v2 库走 ALTER 升级;其余非当前版本(含 v1 与未知版本)一律拒绝启动,
	// 避免旧库被静默建出新表产生"空库可用"的假象。
	if version == 2 {
		if _, err := d.ExecContext(context.Background(), upgradeV2ToV3); err != nil {
			return fmt.Errorf("migrate v2 to v3: %w", err)
		}
		version = 3
	}
	if version == 3 {
		if err := upgradeV3ToV4(d); err != nil {
			return fmt.Errorf("migrate v3 to v4: %w", err)
		}
		version = 4
	} else if version != 0 && version != schemaVersion {
		return fmt.Errorf("database schema is outdated; delete the db file (%s) and restart", path)
	}
	if version == 0 {
		legacy, err := hasLegacyTables(d)
		if err != nil {
			return err
		}
		if legacy {
			return fmt.Errorf("database schema is outdated; delete the db file (%s) and restart", path)
		}
	}
	if _, err := d.ExecContext(context.Background(), migrations); err != nil {
		return err
	}
	_, err := d.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion))
	return err
}

// hasLegacyTables 通过 schema v1(TEXT 主键)就存在的表识别旧库。
// 清单保留旧表名 documents:v2 已更名为 medias,此处匹配的是 v1 旧库。
func hasLegacyTables(d *sql.DB) (bool, error) {
	var count int
	err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN
		('knowledge_bases', 'documents', 'chunks', 'jobs', 'conversations', 'messages', 'app_settings')`).Scan(&count)
	return count > 0, err
}

// upgradeV3ToV4 是 v3 → v4 的增量迁移:jobs 增加去重键,供后台补偿任务防重复入队。
func upgradeV3ToV4(d *sql.DB) error {
	var jobs int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'jobs'`).Scan(&jobs); err != nil {
		return err
	}
	if jobs == 0 {
		return nil
	}
	var dedupe int
	if err := d.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('jobs') WHERE name = 'dedupe_key'`).Scan(&dedupe); err != nil {
		return err
	}
	if dedupe == 0 {
		if _, err := d.ExecContext(context.Background(), `ALTER TABLE jobs ADD COLUMN dedupe_key TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	_, err := d.ExecContext(context.Background(), `CREATE INDEX IF NOT EXISTS idx_jobs_dedupe ON jobs(type, dedupe_key, status)`)
	return err
}
