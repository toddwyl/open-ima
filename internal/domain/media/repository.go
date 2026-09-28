package media

import "context"

// StoredChunk 是持久化到元数据库的分块行;内容本体由检索引擎托管。
type StoredChunk struct {
	BizID      string
	Seq        int
	TokenCount int
}

// Repository 是文档聚合的持久化契约,仅定义接口,实现位于 infrastructure。
type MediaRepository interface {
	Insert(ctx context.Context, doc *Media) error
	// FindIDByHash 按内容哈希查重;未命中返回 ("", nil)。
	FindIDByHash(ctx context.Context, kbBizID, fileHash string) (string, error)
	Get(ctx context.Context, id string) (*Media, error)
	List(ctx context.Context, kbBizID string) ([]Media, error)
	SetStatus(ctx context.Context, id, status string) error
	MarkFailed(ctx context.Context, id, cause string) error
	// ResetFailed 将 failed 文档重置为 pending;返回是否有行被更新。
	ResetFailed(ctx context.Context, id string) (bool, error)
	// MarkDeleting 将非 deleting 文档标记为 deleting;返回是否有行被更新。
	MarkDeleting(ctx context.Context, id string) (bool, error)
	DeleteChunks(ctx context.Context, documentBizID string) error
	// ListChunks 按 seq 升序返回文档的分块定位信息。
	ListChunks(ctx context.Context, documentBizID string) ([]StoredChunk, error)
	ReplaceChunks(ctx context.Context, documentBizID string, chunks []StoredChunk) error
	MarkReady(ctx context.Context, id string, chunkCount int) error
	Delete(ctx context.Context, id string) error
	DeletingIDs(ctx context.Context) ([]string, error)
	// ReconcileCandidates 返回后台对账需要检查的媒体集合。
	ReconcileCandidates(ctx context.Context) ([]Media, error)
	// ReindexableIDs 返回所有非 deleting 状态文档,供全量重建索引。
	ReindexableIDs(ctx context.Context) ([]string, error)
	// ResetForReindex 将文档重置为 pending 并清空错误,供重建索引前调用。
	ResetForReindex(ctx context.Context, id string) error
}
