package db

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenMemoryCreatesTables(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, table := range []string{"knowledge_bases", "medias", "chunks", "jobs", "conversations", "messages"} {
		var name string
		err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
	// 业务表必须是自增 id 主键 + <实体>_biz_id 业务键
	var pkName string
	err = d.QueryRow(`SELECT name FROM pragma_table_info('medias') WHERE pk = 1`).Scan(&pkName)
	if err != nil || pkName != "id" {
		t.Errorf("medias pk = %q, want id (err=%v)", pkName, err)
	}
	var bizCol string
	err = d.QueryRow(`SELECT name FROM pragma_table_info('medias') WHERE name = 'media_biz_id'`).Scan(&bizCol)
	if err != nil {
		t.Errorf("medias missing media_biz_id column: %v", err)
	}
	var version int
	if err := d.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != schemaVersion {
		t.Errorf("user_version = %d, want %d (err=%v)", version, schemaVersion, err)
	}
}

func TestOpenFileIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "test.db")
	d1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d1.Close()
	d2, err := Open(path) // 二次打开重复 migrate 不报错
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	var mode string
	if err := d2.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %s, want wal", mode)
	}
}

// TestOpenRejectsLegacySchema 旧库(user_version=0 且存在 v1 表)不做数据迁移,
// Open 必须报错并提示删除 db 文件。
func TestOpenRejectsLegacySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// schema v1:TEXT 主键,无 user_version
	if _, err := d.Exec(`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE)`); err != nil {
		t.Fatal(err)
	}
	d.Close()

	if _, err := Open(path); err == nil {
		t.Fatal("Open should reject legacy schema")
	} else if !strings.Contains(err.Error(), "delete the db file") {
		t.Fatalf("error should tell user to delete the db file, got: %v", err)
	}
}
