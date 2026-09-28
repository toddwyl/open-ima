// Package websearch 提供联网搜索的 provider 实现;
// 出站请求收敛到固定 provider 域名(SSRF 面与网页抓取同一收敛策略)。
package websearch

import (
	"context"
	"encoding/json"
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

// duckDuckGoAPIURL 是 Instant Answer JSON API,IP 级封禁 html/lite 入口时仍可用的最终兜底。
const duckDuckGoAPIURL = "https://api.duckduckgo.com"

// DuckDuckGo 是免 API key 的 port.WebSearcher 实现:抓取 html.duckduckgo.com 的结果页并解析。
type DuckDuckGo struct {
	hc      *http.Client
	baseURL string
	liteURL string
	apiURL  string
	backoff func(ctx context.Context, d time.Duration) error // 测试可替换
}

func NewDuckDuckGo() *DuckDuckGo {
	return &DuckDuckGo{
		hc:      &http.Client{Timeout: 15 * time.Second},
		baseURL: duckDuckGoBaseURL,
		liteURL: duckDuckGoLiteURL,
		apiURL:  duckDuckGoAPIURL,
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
	// 最终兜底:Instant Answer API(api.duckduckgo.com)。该入口的封禁策略独立于
	// html/lite 结果页,IP 被结果页拉入 202 异常挑战时通常仍可用(对齐 WeKnora 的兜底链)。
	results, err := d.instantAnswer(ctx, query, limit)
	if err == nil && len(results) > 0 {
		return results, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w; instant answer: %v", lastErr, err)
	}
	return nil, lastErr
}

// instantAnswerResponse 是 api.duckduckgo.com Instant Answer 的响应结构(只取需要的字段)。
type instantAnswerResponse struct {
	Heading       string `json:"Heading"`
	AbstractText  string `json:"AbstractText"`
	AbstractURL   string `json:"AbstractURL"`
	Definition    string `json:"Definition"`
	DefinitionURL string `json:"DefinitionURL"`
	Results       []struct {
		Text     string `json:"Text"`
		FirstURL string `json:"FirstURL"`
	} `json:"Results"`
	RelatedTopics []struct {
		Text     string `json:"Text"`
		FirstURL string `json:"FirstURL"`
		Topics   []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"Topics"`
	} `json:"RelatedTopics"`
}

// instantAnswer 调用 Instant Answer API,把摘要/定义/相关主题转成 WebResult。
func (d *DuckDuckGo) instantAnswer(ctx context.Context, query string, limit int) ([]port.WebResult, error) {
	endpoint := d.apiURL + "/?format=json&no_html=1&skip_disambig=1&q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "open-ima/1.0 (knowledge assistant)")
	resp, err := d.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var payload instantAnswerResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	var results []port.WebResult
	add := func(title, link, snippet string) {
		if len(results) >= limit || link == "" || !strings.HasPrefix(link, "http") || strings.Contains(link, "duckduckgo.com") {
			return
		}
		results = append(results, port.WebResult{Title: title, URL: link, Snippet: snippet})
	}
	add(payload.Heading, payload.AbstractURL, payload.AbstractText)
	add(payload.Heading, payload.DefinitionURL, payload.Definition)
	for _, item := range payload.Results {
		title, snippet := splitTopicText(item.Text)
		add(title, item.FirstURL, snippet)
	}
	for _, topic := range payload.RelatedTopics {
		if len(topic.Topics) > 0 {
			for _, sub := range topic.Topics {
				title, snippet := splitTopicText(sub.Text)
				add(title, sub.FirstURL, snippet)
			}
			continue
		}
		title, snippet := splitTopicText(topic.Text)
		add(title, topic.FirstURL, snippet)
	}
	return results, nil
}

// splitTopicText 把 Instant Answer 的 "标题 - 描述" 文本拆成标题与摘要。
func splitTopicText(text string) (title, snippet string) {
	title, snippet, found := strings.Cut(text, " - ")
	if !found {
		return text, ""
	}
	return strings.TrimSpace(title), strings.TrimSpace(snippet)
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
