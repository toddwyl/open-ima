// Package idgen 仅使用标准库生成 UUIDv4 标识符,供领域包创建实体 ID。
package idgen

import (
	"crypto/rand"
	"fmt"
)

// New 返回一个随机 UUIDv4 字符串。
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Errorf("idgen: %w", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
