package websearch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"open-ima/internal/application/port"
)

// baiduBaseURL 是固定的 provider 入口;不允许调用方覆盖生产值,测试可注入。
const baiduBaseURL = "https://www.baidu.com"

// baiduUserAgent 模拟桌面 Chrome;裸 UA 无 cookie 的请求会被百度弹图形验证码。
const baiduUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// Baidu 是免 API key 的 port.WebSearcher 实现,抓取 www.baidu.com 结果页。
//
//	DuckDuckGo 匿名入口对部分出口 IP 长期 202 封禁,百度是中文财经查询的实用替代。
//	百度要求先访问首页拿到 BAIDUID cookie,否则搜索结果页 302 到 wappass 图形验证码,
//	因此首次搜索前做一次首页预热;预热失败不阻断,结果页若仍被跳转则报错。
type Baidu struct {
	hc      *http.Client
	baseURL string
	// resolve 用于解析 baidu.com/link 跳转到真实 URL;测试可注入。
	resolve func(ctx context.Context, link string) string
	// warmup 访问首页播种 cookie;nil 时跳过(测试注入)。
	warmup   func(ctx context.Context) error
	warmOnce sync.Once
}

func NewBaidu() *Baidu {
	jar, _ := cookiejar.New(nil)
	b := &Baidu{
		hc:      &http.Client{Timeout: 15 * time.Second, Jar: jar},
		baseURL: baiduBaseURL,
	}
	b.resolve = b.resolveLink
	b.warmup = b.warmupHome
	return b
}

var _ port.WebSearcher = (*Baidu)(nil)

// warmupHome 访问首页播种 BAIDUID 等 cookie,供后续搜索请求携带。
func (b *Baidu) warmupHome(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", baiduUserAgent)
	resp, err := b.hc.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// Search 返回最多 limit 条结果(标题/URL/摘要);baidu.com/link 跳转会解析为真实 URL。
func (b *Baidu) Search(ctx context.Context, query string, limit int) ([]port.WebResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("web search: query is required")
	}
	if limit <= 0 {
		limit = 5
	}
	// 首次搜索前访问首页播种 BAIDUID cookie;预热失败不阻断,结果页若被弹验证码再报错。
	if b.warmup != nil {
		b.warmOnce.Do(func() { _ = b.warmup(ctx) })
	}
	endpoint := b.baseURL + "/s?ie=utf-8&wd=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", baiduUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Referer", b.baseURL+"/")
	resp, err := b.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("web search: status %d", resp.StatusCode)
	}
	// 跟随跳转后若落在 wappass 验证码页,说明 cookie 预热未生效或被风控。
	if resp.Request.URL != nil && strings.Contains(resp.Request.URL.Host, "wappass.baidu.com") {
		return nil, fmt.Errorf("web search: baidu captcha challenge")
	}
	root, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("web search: parse results: %w", err)
	}
	results := parseBaiduResults(root, limit*2) // 解析多取一些,跳转解析失败时还有余量
	resolveUpTo := min(len(results), limit)
	for index := 0; index < resolveUpTo; index++ {
		if strings.Contains(results[index].URL, "baidu.com/link") {
			if resolved := b.resolve(ctx, results[index].URL); resolved != "" {
				results[index].URL = resolved
			}
		}
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// resolveLink 用禁止跟随跳转的请求取 baidu.com/link 的 Location,失败返回空。
func (b *Baidu) resolveLink(ctx context.Context, link string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", baiduUserAgent)
	var jar http.CookieJar
	if b.hc != nil {
		jar = b.hc.Jar // 携带预热播种的 cookie,避免跳转解析被风控
	}
	client := &http.Client{
		Timeout: 8 * time.Second,
		Jar:     jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	location, err := resp.Location()
	if err != nil {
		return ""
	}
	return location.String()
}

// parseBaiduResults 从结果页提取 div.c-container 容器内的标题链接(h3 a)与摘要文本。
// 百度结果页 class 名带哈希后缀(如 title-wrapper_4oy6O)不稳定,只依赖结构选择器。
func parseBaiduResults(root *html.Node, limit int) []port.WebResult {
	var containers []*html.Node
	var find func(node *html.Node)
	find = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "div" {
			class := attr(node, "class")
			if strings.Contains(class, "c-container") {
				containers = append(containers, node)
				return // 容器不嵌套容器
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			find(child)
		}
	}
	find(root)
	results := make([]port.WebResult, 0, min(len(containers), limit))
	for _, container := range containers {
		if len(results) >= limit {
			break
		}
		titleNode := findFirst(container, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "a" && n.Parent != nil && n.Parent.Data == "h3"
		})
		if titleNode == nil {
			continue
		}
		title := strings.TrimSpace(textContent(titleNode))
		link := attr(titleNode, "href")
		if title == "" || link == "" {
			continue
		}
		snippet := strings.TrimSpace(textContent(container))
		snippet = strings.TrimSpace(strings.TrimPrefix(snippet, title))
		snippet = strings.Join(strings.Fields(snippet), " ") // 归一化空白,去掉多余换行缩进
		snippet = truncateRunesBaidu(snippet, 200)
		results = append(results, port.WebResult{Title: title, URL: link, Snippet: snippet})
	}
	return results
}

// findFirst 深度优先找第一个满足谓词的节点。
func findFirst(node *html.Node, match func(*html.Node) bool) *html.Node {
	if match(node) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findFirst(child, match); found != nil {
			return found
		}
	}
	return nil
}

func truncateRunesBaidu(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
