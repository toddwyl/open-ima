// Package websearch 提供联网搜索的 provider 实现;
// 出站请求收敛到固定 provider 域名(SSRF 面与网页抓取同一收敛策略)。
package websearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"open-ima/internal/application/port"
)

// duckDuckGoBaseURL 是固定的 provider 入口;不允许调用方覆盖生产值,测试可注入。
const duckDuckGoBaseURL = "https://html.duckduckgo.com"

// duckDuckGoLitePath 是被限流(202/429)后的降级入口,同 provider 域名下的精简版页面。
const duckDuckGoLiteURL = "https://lite.duckduckgo.com"

// DuckDuckGo 是免 API key 的 port.WebSearcher 实现:抓取 html.duckduckgo.com 的结果页并解析。
type DuckDuckGo struct {
	hc      *http.Client
	baseURL string
	liteURL string
	backoff func(ctx context.Context, d time.Duration) error // 测试可替换
}

func NewDuckDuckGo() *DuckDuckGo {
	return &DuckDuckGo{
		hc:      &http.Client{Timeout: 15 * time.Second},
		baseURL: duckDuckGoBaseURL,
		liteURL: duckDuckGoLiteURL,
		backoff: sleepWithContext,
	}
}

var _ port.WebSearcher = (*DuckDuckGo)(nil)

// Search 返回最多 limit 条结果(标题/URL/摘要)。
// DuckDuckGo 匿名入口在频繁请求时返回 202 异常挑战,按 2s/4s 退避重试,
// 最后一次尝试降级到 lite 精简页;仍失败则返回最后一个错误。
func (d *DuckDuckGo) Search(ctx context.Context, query string, limit int) ([]port.WebResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("web search: query is required")
	}
	if limit <= 0 {
		limit = 5
	}
	endpoints := []string{d.baseURL + "/html/?q=", d.baseURL + "/html/?q=", d.liteURL + "/lite/?q="}
	delays := []time.Duration{0, 2 * time.Second, 4 * time.Second}
	var lastErr error
	for attempt := range endpoints {
		if attempt > 0 {
			if err := d.backoff(ctx, delays[attempt]); err != nil {
				return nil, err
			}
		}
		results, err := d.fetch(ctx, endpoints[attempt]+url.QueryEscape(query), limit)
		if err == nil && len(results) > 0 {
			return results, nil
		}
		if err != nil {
			lastErr = err
			if !retryableStatus(err) {
				return nil, err
			}
			continue
		}
		lastErr = fmt.Errorf("web search: empty results")
	}
	return nil, lastErr
}

// retryable 标记可重试的限流/挑战状态。
type retryableStatusError struct{ status int }

func (e *retryableStatusError) Error() string { return fmt.Sprintf("web search: status %d", e.status) }

func retryableStatus(err error) bool {
	var statusErr *retryableStatusError
	return errors.As(err, &statusErr)
}

func (d *DuckDuckGo) fetch(ctx context.Context, endpoint string, limit int) ([]port.WebResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36")
	resp, err := d.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
			return nil, &retryableStatusError{status: resp.StatusCode}
		}
		return nil, fmt.Errorf("web search: status %d", resp.StatusCode)
	}
	root, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("web search: parse results: %w", err)
	}
	return parseDuckDuckGoResults(root, limit), nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// parseDuckDuckGoResults 从结果页 DOM 提取 result__a 标题链接与 result__snippet 摘要,按出现顺序配对。
func parseDuckDuckGoResults(root *html.Node, limit int) []port.WebResult {
	var titles []*html.Node
	var snippets []string
	var walk func(node *html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			class := attr(node, "class")
			switch {
			case node.Data == "a" && (strings.Contains(class, "result__a") || strings.Contains(class, "result-link")):
				titles = append(titles, node)
			case strings.Contains(class, "result__snippet") || strings.Contains(class, "result-snippet"):
				snippets = append(snippets, strings.TrimSpace(textContent(node)))
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	results := make([]port.WebResult, 0, min(len(titles), limit))
	for index, node := range titles {
		if len(results) >= limit {
			break
		}
		link := resolveDuckDuckGoLink(attr(node, "href"))
		if link == "" {
			continue
		}
		snippet := ""
		if index < len(snippets) {
			snippet = snippets[index]
		}
		results = append(results, port.WebResult{
			Title: strings.TrimSpace(textContent(node)), URL: link, Snippet: snippet,
		})
	}
	return results
}

// resolveDuckDuckGoLink 把 duckduckgo.com/l/?uddg=... 跳转链接还原为目标 URL。
func resolveDuckDuckGoLink(href string) string {
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if encoded := parsed.Query().Get("uddg"); encoded != "" {
		return encoded
	}
	if parsed.Host != "" && !strings.Contains(parsed.Host, "duckduckgo.com") {
		return href
	}
	return ""
}

func attr(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}

func textContent(node *html.Node) string {
	var text strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return text.String()
}
