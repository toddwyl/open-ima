package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleAnySearchResponse = `{
  "code": 0,
  "message": "success",
  "request_id": "7d6f4e91-2a83-4c5b-9f10-6e8a3d27b541",
  "data": {
    "results": [
      {"title": "大豆价格行情", "url": "https://www.ymt.com/hangqing/juhe-7325", "snippet": "今日大豆最新价格 2.69 元/斤"},
      {"title": "", "url": "", "snippet": "无 URL 应被丢弃"},
      {"title": "CBOT 黄豆期货", "url": "https://gu.sina.cn/ft/hq/hf.php?symbol=S", "snippet": "开盘价 1303.25"}
    ],
    "metadata": {"total_results": 2, "search_time_ms": 312}
  }
}`

func TestAnySearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/search" {
			t.Errorf("request = %s %s", r.Method, r.URL)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["query"] != "大豆价格" || body["max_results"].(float64) != 5 || body["zone"] != "cn" {
			t.Errorf("body = %v", body)
		}
		_, _ = w.Write([]byte(sampleAnySearchResponse))
	}))
	defer server.Close()
	searcher := &AnySearch{hc: server.Client(), baseURL: server.URL, apiKey: "test-key"}
	results, err := searcher.Search(context.Background(), "大豆价格", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	if results[0].Title != "大豆价格行情" || results[0].URL != "https://www.ymt.com/hangqing/juhe-7325" || results[0].Snippet == "" {
		t.Errorf("results[0] = %+v", results[0])
	}
}

func TestAnySearchBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":-1,"message":"Invalid Authorization header or API key."}`))
	}))
	defer server.Close()
	searcher := &AnySearch{hc: server.Client(), baseURL: server.URL, apiKey: "bad"}
	_, err := searcher.Search(context.Background(), "q", 5)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnySearchEmptyQuery(t *testing.T) {
	searcher := NewAnySearch("k")
	if _, err := searcher.Search(context.Background(), "  ", 5); err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestAnySearchLimitCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["max_results"].(float64) != 10 {
			t.Errorf("max_results = %v, want capped 10", body["max_results"])
		}
		_, _ = w.Write([]byte(sampleAnySearchResponse))
	}))
	defer server.Close()
	searcher := &AnySearch{hc: server.Client(), baseURL: server.URL}
	if _, err := searcher.Search(context.Background(), "q", 20); err != nil {
		t.Fatal(err)
	}
}
