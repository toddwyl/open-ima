// Package storage 抽象对象存储(COS 语义),V1 实现为本地目录。
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

type Storage interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	URL(key string) string // 供 parser sidecar 回拉
}

func tokenFor(secret, key string) string {
	sum := sha256.Sum256([]byte(secret + ":" + key))
	return hex.EncodeToString(sum[:])[:16]
}

func validKey(key string) bool {
	if len(key) != 36 {
		return false
	}
	for index, c := range key {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
