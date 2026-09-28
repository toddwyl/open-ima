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

// TestOpenUpgradesV2Schema v2 库经 ALTER TABLE 升级到 v3:会话补 mode 列(旧行归 agent),
// 消息补 agent_steps 列(旧行为 NULL),数据保留。
func TestOpenUpgradesV2Schema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// schema v2:有业务键但无 mode / agent_steps 列
	if _, err := d.Exec(`
		PRAGMA user_version = 2;
		CREATE TABLE knowledge_bases (id INTEGER PRIMARY KEY AUTOINCREMENT, kb_biz_id TEXT NOT NULL UNIQUE, name TEXT NOT NULL UNIQUE);
		CREATE TABLE conversations (id INTEGER PRIMARY KEY AUTOINCREMENT, conversation_biz_id TEXT NOT NULL UNIQUE, kb_biz_id TEXT NOT NULL, title TEXT NOT NULL DEFAULT '');
		CREATE TABLE messages (id INTEGER PRIMARY KEY AUTOINCREMENT, message_biz_id TEXT NOT NULL UNIQUE, conversation_biz_id TEXT NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, citations TEXT NOT NULL DEFAULT '[]');
		INSERT INTO knowledge_bases (kb_biz_id, name) VALUES ('kb1', '库');
		INSERT INTO conversations (conversation_biz_id, kb_biz_id, title) VALUES ('conv1', 'kb1', '旧会话');
		INSERT INTO messages (message_biz_id, conversation_biz_id, role, content) VALUES ('m1', 'conv1', 'assistant', '旧回答');
	`); err != nil {
		t.Fatal(err)
	}
	d.Close()

	upgraded, err := Open(path)
	if err != nil {
		t.Fatalf("Open should upgrade v2 schema: %v", err)
	}
	defer upgraded.Close()
	var version int
	if err := upgraded.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("user_version = %d, want %d (err=%v)", version, schemaVersion, err)
	}
	var mode string
	if err := upgraded.QueryRow(`SELECT mode FROM conversations WHERE conversation_biz_id = 'conv1'`).Scan(&mode); err != nil || mode != "agent" {
		t.Fatalf("mode = %q, want agent (err=%v)", mode, err)
	}
	var steps sql.NullString
	if err := upgraded.QueryRow(`SELECT agent_steps FROM messages WHERE message_biz_id = 'm1'`).Scan(&steps); err != nil || steps.Valid {
		t.Fatalf("agent_steps = %+v, want NULL (err=%v)", steps, err)
	}
}

// TestOpenUpgradesV3Schema v3 库升级到 v4:jobs 补 dedupe_key 列和索引,
// 旧 job 保留且默认 dedupe_key 为空。
func TestOpenUpgradesV3Schema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v3.db")
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`
		PRAGMA user_version = 3;
		CREATE TABLE jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			payload TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			retry_count INTEGER NOT NULL DEFAULT 0,
			run_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX idx_jobs_poll ON jobs(status, run_at);
		INSERT INTO jobs (type, payload) VALUES ('delete_media', '{"media_biz_id":"m1"}');
	`); err != nil {
		t.Fatal(err)
	}
	d.Close()

	upgraded, err := Open(path)
	if err != nil {
		t.Fatalf("Open should upgrade v3 schema: %v", err)
	}
	defer upgraded.Close()
	var version int
	if err := upgraded.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("user_version = %d, want %d (err=%v)", version, schemaVersion, err)
	}
	var dedupeKey string
	if err := upgraded.QueryRow(`SELECT dedupe_key FROM jobs WHERE type = 'delete_media'`).Scan(&dedupeKey); err != nil || dedupeKey != "" {
		t.Fatalf("dedupe_key = %q, want empty (err=%v)", dedupeKey, err)
	}
	var indexName string
	if err := upgraded.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_jobs_dedupe'`).Scan(&indexName); err != nil {
		t.Fatalf("missing idx_jobs_dedupe: %v", err)
	}
}
