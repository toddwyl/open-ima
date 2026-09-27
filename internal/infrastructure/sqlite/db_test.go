package sqlite

import (
	"path/filepath"
	"testing"
)

func TestOpenMemoryCreatesTables(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, table := range []string{"knowledge_bases", "documents", "chunks", "jobs", "conversations", "messages"} {
		var name string
		err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
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
