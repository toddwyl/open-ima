package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"open-ima/internal/application/port"
)

// anySearchBaseURL 是固定的 provider 入口;不允许调用方覆盖生产值,测试可注入。
const anySearchBaseURL = "https://api.anysearch.com"

// AnySearch 是 AnySearch API(https://www.anysearch.com)的 port.WebSearcher 实现。
//
//	POST /v1/search,Bearer 密钥鉴权,返回结构化 JSON,无需 cookie 与页面解析。
//	匿名调用有按 IP 的每日免费额度;稳定使用需在设置中心配置 API key。
type AnySearch struct {
	hc      *http.Client
	baseURL string
	apiKey  string
}

// NewAnySearch 构造携带 API key 的搜索实现;key 为空时按匿名额度调用(极易触发 402)。
func NewAnySearch(apiKey string) *AnySearch {
	return &AnySearch{
		hc:      &http.Client{Timeout: 20 * time.Second},
		baseURL: anySearchBaseURL,
		apiKey:  strings.TrimSpace(apiKey),
	}
}

var _ port.WebSearcher = (*AnySearch)(nil)

type anySearchRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
	Zone       string `json:"zone"`
	Language   string `json:"language"`
}

type anySearchResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
		} `json:"results"`
	} `json:"data"`
}

// Search 返回最多 limit 条结果(标题/URL/摘要);URL 为直达链接,无需二次解析。
func (a *AnySearch) Search(ctx context.Context, query string, limit int) ([]port.WebResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("web search: query is required")
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10 // AnySearch max_results 上限为 10
	}
	payload, err := json.Marshal(anySearchRequest{Query: query, MaxResults: limit, Zone: "cn", Language: "zh-CN"})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/search", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var envelope anySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("web search: decode anysearch response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || envelope.Code != 0 {
		return nil, fmt.Errorf("web search: anysearch status %d code %d: %s", resp.StatusCode, envelope.Code, envelope.Message)
	}
	results := make([]port.WebResult, 0, len(envelope.Data.Results))
	for _, item := range envelope.Data.Results {
		if item.URL == "" {
			continue
		}
		results = append(results, port.WebResult{
			Title:   strings.TrimSpace(item.Title),
			URL:     item.URL,
			Snippet: strings.TrimSpace(item.Snippet),
		})
	}
	return results, nil
}
