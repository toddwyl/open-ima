// Package chat 编排知识库检索用例;对话问答由 application/copilot 的统一引擎承载。
package chat

import (
	"context"
	"fmt"
	"strings"

	"open-ima/internal/application/port"
	"open-ima/internal/domain/knowledgebase"
)

// SearchResult 是一次检索的结果项。
type SearchResult struct {
	ChunkBizID string  `json:"chunk_biz_id"`
	MediaBizID string  `json:"media_biz_id"`
	Title      string  `json:"title"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score"`
}

// Service 是知识库检索用例。
type Service struct {
	kbs       *knowledgebase.KBService
	search    port.Searcher
	indexName string
}

func NewService(kbs *knowledgebase.KBService, searcher port.Searcher, indexName string) *Service {
	return &Service{kbs: kbs, search: searcher, indexName: indexName}
}

// Search 执行单库检索;mode 支持 hybrid(默认)与 text。
func (s *Service) Search(ctx context.Context, kbBizID, query, mode string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if err := s.ensureKnowledgeBase(ctx, kbBizID); err != nil {
		return nil, err
	}
	request := port.SearchRequest{
		Query: query, Filter: "kb_biz_id = '" + escapeFilter(kbBizID) + "'", Limit: 8,
	}
	switch mode {
	case "", "hybrid":
		request.Hybrid = true
	case "text":
	default:
		return nil, fmt.Errorf("unsupported search mode %q", mode)
	}
	hits, err := s.search.Search(ctx, s.indexName, request)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, len(hits))
	for index, hit := range hits {
		snippet := hit.Formatted
		if snippet == "" {
			snippet = hit.Content
		}
		results[index] = SearchResult{
			ChunkBizID: hit.ID, MediaBizID: hit.MediaBizID, Title: hit.Title,
			Snippet: snippet, Score: hit.Score,
		}
	}
	return results, nil
}

func (s *Service) ensureKnowledgeBase(ctx context.Context, kbBizID string) error {
	exists, err := s.kbs.Exists(ctx, kbBizID)
	if err != nil {
		return err
	}
	if !exists {
		return knowledgebase.ErrNotFound
	}
	return nil
}

func escapeFilter(value string) string { return strings.ReplaceAll(value, "'", "''") }
