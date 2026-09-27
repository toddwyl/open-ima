package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"open-ima/internal/application/port"
)

// SearxNG 是自托管元搜索引擎的 port.WebSearcher 实现(JSON API)。
type SearxNG struct {
	hc      *http.Client
	baseURL string
}

// NewSearxNG 以用户配置的实例地址构造;地址校验由设置层完成。
func NewSearxNG(baseURL string) *SearxNG {
	return &SearxNG{hc: &http.Client{Timeout: 15 * time.Second}, baseURL: strings.TrimRight(baseURL, "/")}
}

var _ port.WebSearcher = (*SearxNG)(nil)

// Search 调用 /search?format=json 并映射结果。
func (s *SearxNG) Search(ctx context.Context, query string, limit int) ([]port.WebResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("web search: query is required")
	}
	if limit <= 0 {
		limit = 5
	}
	endpoint := s.baseURL + "/search?format=json&q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "open-ima/1.0")
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("web search: status %d", resp.StatusCode)
	}
	var payload struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("web search: decode: %w", err)
	}
	results := make([]port.WebResult, 0, min(len(payload.Results), limit))
	for _, item := range payload.Results {
		if len(results) >= limit {
			break
		}
		if item.URL == "" {
			continue
		}
		results = append(results, port.WebResult{Title: item.Title, URL: item.URL, Snippet: item.Content})
	}
	return results, nil
}
