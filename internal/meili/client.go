// Package meili provides a small HTTP client for Meilisearch.
package meili

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL      string
	apiKey       string
	hc           *http.Client
	PollInterval time.Duration
	TaskTimeout  time.Duration
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		apiKey:       apiKey,
		hc:           &http.Client{Timeout: 60 * time.Second},
		PollInterval: 200 * time.Millisecond,
		TaskTimeout:  30 * time.Second,
	}
}

type ChunkDoc struct {
	ID         string               `json:"id"`
	KBID       string               `json:"kb_id"`
	DocumentID string               `json:"document_id"`
	Title      string               `json:"title"`
	Content    string               `json:"content"`
	Vectors    map[string][]float32 `json:"_vectors"`
}

type SearchRequest struct {
	Query  string
	Vector []float32
	Filter string
	Limit  int
	Hybrid bool
}

type SearchHit struct {
	ID         string
	KBID       string
	DocumentID string
	Title      string
	Content    string
	Formatted  string
	Score      float64
}

type taskResponse struct {
	TaskUID int64 `json:"taskUid"`
}

func (c *Client) EnsureIndex(ctx context.Context, uid string, dimensions int) error {
	status, err := c.do(ctx, http.MethodGet, "/indexes/"+uid, nil, nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		var task taskResponse
		status, err = c.do(ctx, http.MethodPost, "/indexes", map[string]string{
			"uid": uid, "primaryKey": "id",
		}, &task)
		if err := writeResult(http.MethodPost, "/indexes", status, err); err != nil {
			return err
		}
		if err := c.waitTask(ctx, task.TaskUID); err != nil {
			return err
		}
	}

	settings := map[string]any{
		"searchableAttributes": []string{"title", "content"},
		"filterableAttributes": []string{"kb_id", "document_id"},
		"embedders": map[string]any{
			"default": map[string]any{"source": "userProvided", "dimensions": dimensions},
		},
	}
	var task taskResponse
	path := "/indexes/" + uid + "/settings"
	status, err = c.do(ctx, http.MethodPatch, path, settings, &task)
	if err := writeResult(http.MethodPatch, path, status, err); err != nil {
		return err
	}
	return c.waitTask(ctx, task.TaskUID)
}

func (c *Client) AddDocuments(ctx context.Context, uid string, docs []ChunkDoc) error {
	var task taskResponse
	path := "/indexes/" + uid + "/documents"
	status, err := c.do(ctx, http.MethodPost, path, docs, &task)
	if err := writeResult(http.MethodPost, path, status, err); err != nil {
		return err
	}
	return c.waitTask(ctx, task.TaskUID)
}

func (c *Client) DeleteByFilter(ctx context.Context, uid, filter string) error {
	var task taskResponse
	path := "/indexes/" + uid + "/documents/delete"
	status, err := c.do(ctx, http.MethodPost, path, map[string]string{"filter": filter}, &task)
	if err := writeResult(http.MethodPost, path, status, err); err != nil {
		return err
	}
	return c.waitTask(ctx, task.TaskUID)
}

func (c *Client) Search(ctx context.Context, uid string, request SearchRequest) ([]SearchHit, error) {
	limit := request.Limit
	if limit <= 0 {
		limit = 8
	}
	body := map[string]any{
		"q": request.Query, "filter": request.Filter, "limit": limit,
		"attributesToHighlight": []string{"content"}, "showRankingScore": true,
	}
	if request.Hybrid {
		body["vector"] = request.Vector
		body["hybrid"] = map[string]any{"semanticRatio": 0.5, "embedder": "default"}
	}
	var response struct {
		Hits []struct {
			ID         string `json:"id"`
			KBID       string `json:"kb_id"`
			DocumentID string `json:"document_id"`
			Title      string `json:"title"`
			Content    string `json:"content"`
			Formatted  struct {
				Content string `json:"content"`
			} `json:"_formatted"`
			Score float64 `json:"_rankingScore"`
		} `json:"hits"`
	}
	path := "/indexes/" + uid + "/search"
	status, err := c.do(ctx, http.MethodPost, path, body, &response)
	if err := writeResult(http.MethodPost, path, status, err); err != nil {
		return nil, err
	}
	hits := make([]SearchHit, len(response.Hits))
	for index, hit := range response.Hits {
		hits[index] = SearchHit{
			ID: hit.ID, KBID: hit.KBID, DocumentID: hit.DocumentID,
			Title: hit.Title, Content: hit.Content, Formatted: hit.Formatted.Content, Score: hit.Score,
		}
	}
	return hits, nil
}

func writeResult(method, path string, status int, err error) error {
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("meili %s %s: status %d", method, path, status)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		return resp.StatusCode, fmt.Errorf("meili %s %s: status %d: %s", method, path, resp.StatusCode, data)
	}
	if resp.StatusCode < 300 && out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("meili decode %s %s: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

func (c *Client) waitTask(ctx context.Context, taskUID int64) error {
	timer := time.NewTimer(c.TaskTimeout)
	defer timer.Stop()
	ticker := time.NewTicker(c.PollInterval)
	defer ticker.Stop()

	for {
		var task struct {
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		path := fmt.Sprintf("/tasks/%d", taskUID)
		status, err := c.do(ctx, http.MethodGet, path, nil, &task)
		if err := writeResult(http.MethodGet, path, status, err); err != nil {
			return err
		}
		switch task.Status {
		case "succeeded":
			return nil
		case "failed", "canceled":
			message := "unknown"
			if task.Error != nil {
				message = task.Error.Message
			}
			return fmt.Errorf("meili task %d %s: %s", taskUID, task.Status, message)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("meili task %d: timeout after %s", taskUID, c.TaskTimeout)
		case <-ticker.C:
		}
	}
}
