package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type LocalStorage struct {
	root       string
	publicBase string
	secret     string
}

var _ Storage = (*LocalStorage)(nil)

func NewLocalStorage(root, publicBase, secret string) (*LocalStorage, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &LocalStorage{root: root, publicBase: publicBase, secret: secret}, nil
}

func (s *LocalStorage) path(key string) string {
	return filepath.Join(s.root, key[:2], key)
}

func (s *LocalStorage) Put(_ context.Context, key string, r io.Reader) error {
	if !validKey(key) {
		return fmt.Errorf("invalid storage key %q", key)
	}
	dir := filepath.Dir(s.path(key))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path(key))
}

func (s *LocalStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if !validKey(key) {
		return nil, fmt.Errorf("invalid storage key %q", key)
	}
	return os.Open(s.path(key))
}

func (s *LocalStorage) Delete(_ context.Context, key string) error {
	if !validKey(key) {
		return fmt.Errorf("invalid storage key %q", key)
	}
	if err := os.Remove(s.path(key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (s *LocalStorage) URL(key string) string {
	return fmt.Sprintf("%s/internal/files/%s?token=%s", s.publicBase, key, tokenFor(s.secret, key))
}

// Handler exposes GET /internal/files/{key}?token=... for parser sidecar callbacks.
func (s *LocalStorage) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if key == "" {
			key = strings.TrimPrefix(r.URL.Path, "/internal/files/")
		}
		if !validKey(key) {
			http.Error(w, "invalid key", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("token") != tokenFor(s.secret, key) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.ServeFile(w, r, s.path(key))
	})
}
