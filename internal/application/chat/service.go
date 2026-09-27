// Package chat 编排检索与对话用例:查询改写、混合检索、RRF 融合与流式回答。
package chat

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"open-ima/internal/application/port"
	"open-ima/internal/domain/conversation"
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

// Service 是检索与对话用例。
type Service struct {
	conv      *conversation.ConversationService
	kbs       *knowledgebase.KBService
	search    port.Searcher
	indexName string

	mu                sync.RWMutex
	models            map[string]port.ChatModel
	defaultModelBizID string
}

func NewService(conv *conversation.ConversationService, kbs *knowledgebase.KBService, searcher port.Searcher, model port.ChatModel, indexName string) *Service {
	return &Service{conv: conv, kbs: kbs, search: searcher, models: map[string]port.ChatModel{"default": model}, defaultModelBizID: "default", indexName: indexName}
}

// SetModels 原子替换可选聊天模型集合。
func (s *Service) SetModels(models map[string]port.ChatModel, defaultModelBizID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.models = models
	s.defaultModelBizID = defaultModelBizID
}

func (s *Service) chatModel(modelBizID string) (port.ChatModel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if modelBizID == "" {
		modelBizID = s.defaultModelBizID
	}
	model := s.models[modelBizID]
	if model == nil {
		return nil, fmt.Errorf("chat model %q is not configured", modelBizID)
	}
	return model, nil
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

// Chat 执行 RAG 对话:改写问题、双路检索融合、流式生成并落库。
func (s *Service) Chat(ctx context.Context, kbBizID, conversationBizID, modelBizID, query string, onToken func(string) error) (string, []conversation.Citation, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil, fmt.Errorf("query is required")
	}
	var conv *conversation.Conversation
	if conversationBizID == "" {
		if err := s.ensureKnowledgeBase(ctx, kbBizID); err != nil {
			return "", nil, err
		}
	}
	var err error
	conv, err = s.conv.Ensure(ctx, kbBizID, conversationBizID, query)
	if err != nil {
		return "", nil, err
	}
	history, err := s.conv.RecentMessages(ctx, conv.BizID, 10)
	if err != nil {
		return conv.BizID, nil, err
	}
	model, err := s.chatModel(modelBizID)
	if err != nil {
		return conv.BizID, nil, err
	}
	rewritten := s.rewrite(ctx, model, query, history)
	citations, err := s.retrieve(ctx, kbBizID, query, rewritten)
	if err != nil {
		return conv.BizID, nil, err
	}
	if err := s.conv.Append(ctx, conv.BizID, "user", query, nil); err != nil {
		return conv.BizID, nil, err
	}

	messages := buildAnswerMessages(history, query, citations)
	var answer strings.Builder
	err = model.Stream(ctx, messages, func(token string) error {
		answer.WriteString(token)
		return onToken(token)
	})
	if err != nil {
		return conv.BizID, citations, err
	}
	if err := s.conv.Append(ctx, conv.BizID, "assistant", answer.String(), citations); err != nil {
		return conv.BizID, citations, err
	}
	return conv.BizID, citations, nil
}

func (s *Service) ListConversations(ctx context.Context, kbBizID string) ([]conversation.Conversation, error) {
	return s.conv.ListByKB(ctx, kbBizID)
}

func (s *Service) ListMessages(ctx context.Context, conversationBizID string) ([]conversation.Message, error) {
	return s.conv.ListMessages(ctx, conversationBizID)
}

func (s *Service) rewrite(ctx context.Context, model port.ChatModel, query string, history []conversation.Message) string {
	start := max(0, len(history)-4)
	var contextLines []string
	for _, message := range history[start:] {
		contextLines = append(contextLines, message.Role+": "+message.Content)
	}
	prompt := "Rewrite the latest user question as one standalone search query. Return only the query.\n"
	if len(contextLines) > 0 {
		prompt += "Recent conversation:\n" + strings.Join(contextLines, "\n") + "\n"
	}
	prompt += "Question: " + query
	rewritten, err := model.Complete(ctx, []port.ChatMessage{
		{Role: "system", Content: "You rewrite questions for media retrieval."},
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(rewritten)
}

func (s *Service) retrieve(ctx context.Context, kbBizID, query, rewritten string) ([]conversation.Citation, error) {
	queries := []string{query}
	if rewritten != "" && !strings.EqualFold(rewritten, query) {
		queries = append(queries, rewritten)
	}
	ranked := make([][]port.SearchHit, len(queries))
	for index, searchQuery := range queries {
		var err error
		ranked[index], err = s.search.Search(ctx, s.indexName, port.SearchRequest{
			Query: searchQuery, Filter: "kb_biz_id = '" + escapeFilter(kbBizID) + "'", Limit: 8, Hybrid: true,
		})
		if err != nil {
			return nil, err
		}
	}
	return fuse(ranked, 8), nil
}

func fuse(rankings [][]port.SearchHit, limit int) []conversation.Citation {
	type scored struct {
		hit   port.SearchHit
		score float64
	}
	byID := make(map[string]scored)
	for _, hits := range rankings {
		for rank, hit := range hits {
			item := byID[hit.ID]
			item.hit = hit
			item.score += 1.0 / float64(60+rank+1)
			byID[hit.ID] = item
		}
	}
	items := make([]scored, 0, len(byID))
	for _, item := range byID {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].hit.ID < items[j].hit.ID
		}
		return items[i].score > items[j].score
	})
	if len(items) > limit {
		items = items[:limit]
	}
	citations := make([]conversation.Citation, len(items))
	for index, item := range items {
		snippet := item.hit.Formatted
		if snippet == "" {
			snippet = item.hit.Content
		}
		citations[index] = conversation.Citation{
			MediaBizID: item.hit.MediaBizID, Title: item.hit.Title, ChunkBizID: item.hit.ID,
			Snippet: snippet, Score: item.score,
		}
	}
	return citations
}

func buildAnswerMessages(history []conversation.Message, query string, citations []conversation.Citation) []port.ChatMessage {
	var sources strings.Builder
	for index, citation := range citations {
		fmt.Fprintf(&sources, "[%d] %s\n%s\n\n", index+1, citation.Title, citation.Snippet)
	}
	messages := []port.ChatMessage{{
		Role:    "system",
		Content: "Answer from the provided sources. Cite factual claims with source numbers like [1]. Say when the sources do not contain the answer.\n\nSources:\n" + sources.String(),
	}}
	start := max(0, len(history)-10)
	for _, message := range history[start:] {
		messages = append(messages, port.ChatMessage{Role: message.Role, Content: message.Content})
	}
	return append(messages, port.ChatMessage{Role: "user", Content: query})
}

func escapeFilter(value string) string { return strings.ReplaceAll(value, "'", "''") }
