// Package llm wraps OpenAI-compatible model APIs.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type EmbeddingClient struct {
	baseURL   string
	apiKey    string
	model     string
	hc        *http.Client
	BatchSize int
}

func NewEmbeddingClient(baseURL, apiKey, model string) *EmbeddingClient {
	return &EmbeddingClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		model:     model,
		hc:        &http.Client{Timeout: 120 * time.Second},
		BatchSize: 64,
	}
}

func (c *EmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if c.BatchSize <= 0 {
		return nil, fmt.Errorf("embedding: batch size must be positive")
	}
	vectors := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += c.BatchSize {
		end := min(start+c.BatchSize, len(texts))
		batch, err := c.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, batch...)
	}
	return vectors, nil
}

func (c *EmbeddingClient) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(map[string]any{"model": c.model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding: status %d", resp.StatusCode)
	}

	var response struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("embedding: decode: %w", err)
	}
	if len(response.Data) != len(texts) {
		return nil, fmt.Errorf("embedding: got %d vectors for %d inputs", len(response.Data), len(texts))
	}
	vectors := make([][]float32, len(texts))
	for _, item := range response.Data {
		if item.Index < 0 || item.Index >= len(texts) {
			return nil, fmt.Errorf("embedding: response index %d out of range", item.Index)
		}
		if vectors[item.Index] != nil {
			return nil, fmt.Errorf("embedding: duplicate response index %d", item.Index)
		}
		vectors[item.Index] = item.Embedding
	}
	return vectors, nil
}
