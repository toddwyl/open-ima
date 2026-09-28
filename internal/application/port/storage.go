package port

import (
	"context"
	"io"
	"time"
)

// FileStore 是对象存储端口(COS 语义):按内容寻址存放源文件。
type FileStore interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	// URL 返回供 parser sidecar 回拉的地址。
	URL(key string) string
}

// StoredObject 是对象存储中的一项,用于后台对账读取外部投影视图。
type StoredObject struct {
	Key       string
	Size      int64
	UpdatedAt time.Time
}

// FileStoreInspector 提供对象存在性与分页枚举能力,仅供内部对账使用。
type FileStoreInspector interface {
	Exists(ctx context.Context, key string) (bool, error)
	List(ctx context.Context, prefix string, cursor string, limit int) ([]StoredObject, string, error)
}

// Fetcher 抓取公网页面内容;实现负责大小限制与状态码校验。
type Fetcher interface {
	Fetch(ctx context.Context, rawURL string) ([]byte, error)
}
