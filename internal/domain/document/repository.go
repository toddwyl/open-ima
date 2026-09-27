package document

import "context"

// StoredChunk 是持久化到元数据库的分块行;内容本体由检索引擎托管。
type StoredChunk struct {
	ID         string
	Seq        int
	TokenCount int
}

// Repository 是文档聚合的持久化契约,仅定义接口,实现位于 infrastructure。
type DocumentRepository interface {
	Insert(ctx context.Context, doc *Document) error
	// FindIDByHash 按内容哈希查重;未命中返回 ("", nil)。
	FindIDByHash(ctx context.Context, kbID, fileHash string) (string, error)
	Get(ctx context.Context, id string) (*Document, error)
	List(ctx context.Context, kbID string) ([]Document, error)
	SetStatus(ctx context.Context, id, status string) error
	MarkFailed(ctx context.Context, id, cause string) error
	// ResetFailed 将 failed 文档重置为 pending;返回是否有行被更新。
	ResetFailed(ctx context.Context, id string) (bool, error)
	// MarkDeleting 将非 deleting 文档标记为 deleting;返回是否有行被更新。
	MarkDeleting(ctx context.Context, id string) (bool, error)
	DeleteChunks(ctx context.Context, documentID string) error
	ReplaceChunks(ctx context.Context, documentID string, chunks []StoredChunk) error
	MarkReady(ctx context.Context, id string, chunkCount int) error
	Delete(ctx context.Context, id string) error
	DeletingIDs(ctx context.Context) ([]string, error)
}
