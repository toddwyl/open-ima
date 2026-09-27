package port

import "context"

// ChunkDoc 是写入检索引擎的分块文档;向量由检索引擎托管生成。
// ID 是检索引擎的文档主键(引擎级命名,值为分块业务键)。
type ChunkDoc struct {
	ID         string `json:"id"`
	KBBizID    string `json:"kb_biz_id"`
	MediaBizID string `json:"media_biz_id"`
	Title      string `json:"title"`
	Content    string `json:"content"`
}

// SearchRequest 是一次检索请求;Hybrid 表示关键词+向量混合检索。
type SearchRequest struct {
	Query  string
	Filter string
	Limit  int
	Hybrid bool
}

// SearchHit 是一条检索命中。ID 是检索引擎的文档主键(值为分块业务键)。
type SearchHit struct {
	ID         string
	KBBizID    string
	MediaBizID string
	Title      string
	Content    string
	Formatted  string
	Score      float64
}

// EmbedderConfig 描述检索引擎托管的 embedding 配置。
type EmbedderConfig struct {
	URL        string
	Model      string
	Dimensions int
}

// Indexer 是检索引擎的写入端口。
type Indexer interface {
	AddDocuments(ctx context.Context, index string, docs []ChunkDoc) error
	DeleteByFilter(ctx context.Context, index, filter string) error
}

// Searcher 是检索引擎的查询端口。
type Searcher interface {
	Search(ctx context.Context, index string, request SearchRequest) ([]SearchHit, error)
}

// SearchAdmin 是检索引擎的管理端口。
type SearchAdmin interface {
	EnsureIndex(ctx context.Context, uid string, embedder EmbedderConfig) error
}
