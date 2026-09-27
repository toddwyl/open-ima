package document

import "time"

// 文档生命周期状态。
const (
	StatusPending  = "pending"
	StatusParsing  = "parsing"
	StatusChunking = "chunking"
	StatusIndexing = "indexing"
	StatusReady    = "ready"
	StatusFailed   = "failed"
	StatusDeleting = "deleting"
)

// Document 是文档聚合根,记录来源与生命周期状态。实体仅携带业务键;自增主键留在 DB 层。
type Document struct {
	BizID      string    `json:"biz_id"`
	KBBizID    string    `json:"kb_biz_id"`
	Title      string    `json:"title"`
	SourceType string    `json:"source_type"`
	SourceURI  string    `json:"source_uri"`
	FileType   string    `json:"file_type"`
	FileHash   string    `json:"file_hash"`
	Status     string    `json:"status"`
	Error      string    `json:"error"`
	ChunkCount int       `json:"chunk_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
