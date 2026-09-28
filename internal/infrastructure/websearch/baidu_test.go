package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestBaidu 构造测试用 searcher:baseURL 指向 httptest,跳转解析注入假函数。
func newTestBaidu(server *httptest.Server, resolve func(ctx context.Context, link string) string) *Baidu {
	if resolve == nil {
		resolve = func(context.Context, string) string { return "" }
	}
	return &Baidu{hc: server.Client(), baseURL: server.URL, resolve: resolve}
}

const sampleBaiduPage = `<!doctype html><html><body>
<div id="content_left">
<div class="result c-container" tpl="se_com_default">
  <h3 class="t"><a href="https://www.baidu.com/link?url=abc123">大豆周报:<em> USDA</em>供需解读</a></h3>
  <div class="c-abstract">美豆单产上调至 53.1 蒲式耳,期末库存高于预期,<span>价格承压</span>。</div>
</div>
<div class="result c-container">
  <h3><a href="https://finance.example.com/soy-price">大豆现货价格最新行情</a></h3>
  <div>现货报价 4200 元/吨,环比下跌 1.2%。</div>
</div>
<div class="result c-container">
  <div>没有标题链接的容器应被跳过</div>
</div>
</div>
</body></html>`

func TestBaiduSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/s" || r.URL.Query().Get("wd") != "大豆价格" {
			t.Errorf("request = %s", r.URL)
		}
		_, _ = w.Write([]byte(sampleBaiduPage))
	}))
	defer server.Close()
	searcher := newTestBaidu(server, func(_ context.Context, link string) string {
		if link == "https://www.baidu.com/link?url=abc123" {
			return "https://news.example.com/usda-report"
		}
		return ""
	})
	results, err := searcher.Search(context.Background(), "大豆价格", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	if results[0].Title != "大豆周报: USDA供需解读" {
		t.Errorf("title = %q", results[0].Title)
	}
	if results[0].URL != "https://news.example.com/usda-report" {
		t.Errorf("redirect URL not resolved: %q", results[0].URL)
	}
	if !strings.Contains(results[0].Snippet, "期末库存高于预期") || strings.Contains(results[0].Snippet, "大豆周报") {
		t.Errorf("snippet = %q", results[0].Snippet)
	}
	// 第二条是直达链接,不应调用跳转解析。
	if results[1].URL != "https://finance.example.com/soy-price" {
		t.Errorf("results[1].URL = %q", results[1].URL)
	}
}

func TestBaiduSearchResolveFailureKeepsOriginalLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sampleBaiduPage))
	}))
	defer server.Close()
	searcher := newTestBaidu(server, nil) // 解析失败返回空
	results, err := searcher.Search(context.Background(), "q", 5)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].URL != "https://www.baidu.com/link?url=abc123" {
		t.Errorf("results[0].URL = %q", results[0].URL)
	}
}

func TestBaiduSearchLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sampleBaiduPage))
	}))
	defer server.Close()
	searcher := newTestBaidu(server, nil)
	results, err := searcher.Search(context.Background(), "q", 1)
	if err != nil || len(results) != 1 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestBaiduSearchHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	defer server.Close()
	searcher := newTestBaidu(server, nil)
	_, err := searcher.Search(context.Background(), "q", 5)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
}

func TestBaiduSearchEmptyQuery(t *testing.T) {
	searcher := newTestBaidu(httptest.NewServer(http.NotFoundHandler()), nil)
	if _, err := searcher.Search(context.Background(), "  ", 5); err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestBaiduResolveLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/link" {
			http.Redirect(w, r, "https://real.example.com/article", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	b := &Baidu{}
	if got := b.resolveLink(context.Background(), server.URL+"/link?url=x"); got != "https://real.example.com/article" {
		t.Errorf("resolved = %q", got)
	}
	if got := b.resolveLink(context.Background(), server.URL+"/missing"); got != "" {
		t.Errorf("expected empty for non-redirect, got %q", got)
	}
}
