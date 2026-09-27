// Package websearch 提供联网搜索的 provider 实现;
// 出站请求收敛到固定 provider 域名(SSRF 面与网页抓取同一收敛策略)。
package websearch

import (
	"context"
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

// DuckDuckGo 是免 API key 的 port.WebSearcher 实现:抓取 html.duckduckgo.com 的结果页并解析。
type DuckDuckGo struct {
	hc      *http.Client
	baseURL string
}

func NewDuckDuckGo() *DuckDuckGo {
	return &DuckDuckGo{hc: &http.Client{Timeout: 15 * time.Second}, baseURL: duckDuckGoBaseURL}
}

var _ port.WebSearcher = (*DuckDuckGo)(nil)

// Search 返回最多 limit 条结果(标题/URL/摘要)。
func (d *DuckDuckGo) Search(ctx context.Context, query string, limit int) ([]port.WebResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("web search: query is required")
	}
	if limit <= 0 {
		limit = 5
	}
	endpoint := d.baseURL + "/html/?q=" + url.QueryEscape(query)
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
		return nil, fmt.Errorf("web search: status %d", resp.StatusCode)
	}
	root, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("web search: parse results: %w", err)
	}
	return parseDuckDuckGoResults(root, limit), nil
}

// parseDuckDuckGoResults 从结果页 DOM 提取 result__a 标题链接与 result__snippet 摘要,按出现顺序配对。
func parseDuckDuckGoResults(root *html.Node, limit int) []port.WebResult {
	var titles []*html.Node
	var snippets []string
	var walk func(node *html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			class := attr(node, "class")
			switch {
			case strings.Contains(class, "result__a"):
				titles = append(titles, node)
			case strings.Contains(class, "result__snippet"):
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
