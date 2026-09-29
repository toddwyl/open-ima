package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testKey = "01234567-89ab-4def-8123-456789abcdef"

func newTestStore(t *testing.T) *LocalStorage {
	t.Helper()
	s, err := NewLocalStorage(filepath.Join(t.TempDir(), "files"), "http://app:8080", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPutGetDeleteRoundtrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Put(ctx, testKey, strings.NewReader("hello world")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.root, testKey[:2], testKey)); err != nil {
		t.Fatalf("file not at sharded path: %v", err)
	}
	rc, err := s.Get(ctx, testKey)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "hello world" {
		t.Fatalf("got %q", data)
	}
	if err := s.Delete(ctx, testKey); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, testKey); err != nil { // 幂等
		t.Fatalf("second delete: %v", err)
	}
	if _, err := s.Get(ctx, testKey); err == nil {
		t.Fatal("get after delete should fail")
	}
}

func TestExistsAndListObjects(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	otherKey := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if exists, err := s.Exists(ctx, testKey); err != nil || exists {
		t.Fatalf("missing exists=%v err=%v, want false nil", exists, err)
	}
	if err := s.Put(ctx, testKey, strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, otherKey, strings.NewReader("world")); err != nil {
		t.Fatal(err)
	}
	if exists, err := s.Exists(ctx, testKey); err != nil || !exists {
		t.Fatalf("existing exists=%v err=%v, want true nil", exists, err)
	}
	page, cursor, err := s.List(ctx, "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].Key == "" || cursor == "" {
		t.Fatalf("first page=%+v cursor=%q", page, cursor)
	}
	next, cursor, err := s.List(ctx, "", cursor, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 1 || cursor != "" {
		t.Fatalf("second page=%+v cursor=%q", next, cursor)
	}
	keys := map[string]bool{page[0].Key: true, next[0].Key: true}
	if !keys[testKey] || !keys[otherKey] {
		t.Fatalf("listed keys = %v", keys)
	}
}

func TestRejectsInvalidKey(t *testing.T) {
	s := newTestStore(t)
	if err := s.Put(context.Background(), "../evil", strings.NewReader("x")); err == nil {
		t.Fatal("expected error for non-hex key")
	}
}

func TestURLAndHandlerToken(t *testing.T) {
	s := newTestStore(t)
	if err := s.Put(context.Background(), testKey, strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	url := s.URL(testKey)
	if !strings.HasPrefix(url, "http://app:8080/internal/files/"+testKey+"?token=") {
		t.Fatalf("url = %q", url)
	}
	token := strings.Split(url, "token=")[1]
	sum := sha256.Sum256([]byte("test-secret" + ":" + testKey))
	if token != hex.EncodeToString(sum[:])[:16] {
		t.Fatalf("token mismatch")
	}
	h := s.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/internal/files/"+testKey+"?token="+token, nil))
	if rec.Code != 200 || rec.Body.String() != "data" {
		t.Fatalf("valid token: code=%d body=%q", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/internal/files/"+testKey+"?token=bad", nil))
	if rec.Code != 403 {
		t.Fatalf("bad token: code=%d", rec.Code)
	}
}
