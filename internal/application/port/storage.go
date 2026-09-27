package port

import (
	"context"
	"io"
)

// FileStore 是对象存储端口(COS 语义):按内容寻址存放源文件。
type FileStore interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	// URL 返回供 parser sidecar 回拉的地址。
	URL(key string) string
}

// Fetcher 抓取公网页面内容;实现负责大小限制与状态码校验。
type Fetcher interface {
	Fetch(ctx context.Context, rawURL string) ([]byte, error)
}
