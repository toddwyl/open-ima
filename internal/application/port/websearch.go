package port

import "context"

// WebResult 是一条联网搜索结果。
type WebResult struct {
	Title   string
	URL     string
	Snippet string
}

// WebSearcher 是联网搜索端口;实现须把出站请求收敛到固定 provider 域名。
type WebSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]WebResult, error)
}
