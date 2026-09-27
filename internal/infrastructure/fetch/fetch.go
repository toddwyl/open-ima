// Package fetch 抓取公网页面内容,负责大小限制与状态码校验。
package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"open-ima/internal/application/port"
)

const defaultMaxBytes = 10 << 20

// Fetcher 是 port.Fetcher 的 HTTP 实现。
type Fetcher struct {
	hc       *http.Client
	maxBytes int64
}

func New() *Fetcher {
	return &Fetcher{
		hc:       &http.Client{Timeout: 10 * time.Second},
		maxBytes: defaultMaxBytes,
	}
}

var _ port.Fetcher = (*Fetcher)(nil)

// Fetch 拉取页面正文;非 200 或超过 10MB 限制时返回错误。
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "open-ima/1.0")
	resp, err := f.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", rawURL, resp.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > f.maxBytes {
		return nil, fmt.Errorf("fetch %s: page exceeds 10MB limit", rawURL)
	}
	return content, nil
}
