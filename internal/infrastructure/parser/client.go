// Package parserclient calls the Python parser sidecar.
package parser

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

// 解析类型统一定义在 application/port,此处以别名保持客户端签名即端口实现。
type Block = port.Block

type ParseResult = port.ParseResult

// FatalError means the input cannot be parsed and retrying will not help.
type FatalError = port.FatalError

var _ port.Parser = (*Client)(nil)

type Client struct {
	baseURL string
	hc      *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		hc:      &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *Client) Parse(ctx context.Context, fileURL, fileType string) (*ParseResult, error) {
	body, err := json.Marshal(map[string]string{"file_url": fileURL, "file_type": fileType})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/parse", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var response struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&response)
		if response.Error == "" {
			response.Error = http.StatusText(resp.StatusCode)
		}
		if resp.StatusCode == http.StatusUnprocessableEntity {
			return nil, &FatalError{Message: response.Error}
		}
		return nil, fmt.Errorf("parser: status %d: %s", resp.StatusCode, response.Error)
	}

	var result ParseResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parser: decode: %w", err)
	}
	return &result, nil
}
