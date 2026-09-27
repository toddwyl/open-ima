package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleResultsPage = `<!doctype html><html><body>
<div class="result">
  <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fnews-1&amp;rut=abc">示例新闻一</a>
  <a class="result__snippet">第一条的<b>摘要</b>文本。</a>
</div>
<div class="result">
  <a rel="nofollow" class="result__a" href="https://direct.example.org/page">直达链接二</a>
  <a class="result__snippet">第二条摘要。</a>
</div>
</body></html>`

func TestDuckDuckGoSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/html/" || r.URL.Query().Get("q") != "open ima" {
			t.Errorf("request = %s", r.URL)
		}
		_, _ = w.Write([]byte(sampleResultsPage))
	}))
	defer server.Close()
	searcher := &DuckDuckGo{hc: server.Client(), baseURL: server.URL}
	results, err := searcher.Search(context.Background(), "open ima", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	if results[0].URL != "https://example.com/news-1" || results[0].Title != "示例新闻一" || results[0].Snippet != "第一条的摘要文本。" {
		t.Errorf("results[0] = %+v", results[0])
	}
	if results[1].URL != "https://direct.example.org/page" {
		t.Errorf("results[1] = %+v", results[1])
	}
}

func TestDuckDuckGoLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sampleResultsPage))
	}))
	defer server.Close()
	searcher := &DuckDuckGo{hc: server.Client(), baseURL: server.URL}
	results, err := searcher.Search(context.Background(), "q", 1)
	if err != nil || len(results) != 1 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestDuckDuckGoHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "anomaly", http.StatusForbidden)
	}))
	defer server.Close()
	searcher := &DuckDuckGo{hc: server.Client(), baseURL: server.URL}
	_, err := searcher.Search(context.Background(), "q", 5)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
}

func TestDuckDuckGoEmptyQuery(t *testing.T) {
	_, err := NewDuckDuckGo().Search(context.Background(), "  ", 5)
	if err == nil {
		t.Fatal("empty query must fail")
	}
}
