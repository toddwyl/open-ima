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
	ChunkID    string  `json:"chunk_id"`
	DocumentID string  `json:"document_id"`
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

	mu    sync.RWMutex
	model port.ChatModel
}

func NewService(conv *conversation.ConversationService, kbs *knowledgebase.KBService, searcher port.Searcher, model port.ChatModel, indexName string) *Service {
	return &Service{conv: conv, kbs: kbs, search: searcher, model: model, indexName: indexName}
}

// SetModel 替换聊天模型;实现 settings 用例的 ChatReconfigurer。
func (s *Service) SetModel(model port.ChatModel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = model
}

func (s *Service) chatModel() port.ChatModel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.model
}

// Search 执行单库检索;mode 支持 hybrid(默认)与 text。
func (s *Service) Search(ctx context.Context, kbID, query, mode string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if err := s.ensureKnowledgeBase(ctx, kbID); err != nil {
		return nil, err
	}
	request := port.SearchRequest{
		Query: query, Filter: "kb_biz_id = '" + escapeFilter(kbID) + "'", Limit: 8,
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
			ChunkID: hit.ID, DocumentID: hit.DocumentID, Title: hit.Title,
			Snippet: snippet, Score: hit.Score,
		}
	}
	return results, nil
}

func (s *Service) ensureKnowledgeBase(ctx context.Context, kbID string) error {
	exists, err := s.kbs.Exists(ctx, kbID)
	if err != nil {
		return err
	}
	if !exists {
		return knowledgebase.ErrNotFound
	}
	return nil
}

// Chat 执行 RAG 对话:改写问题、双路检索融合、流式生成并落库。
func (s *Service) Chat(ctx context.Context, kbID, conversationID, query string, onToken func(string) error) (string, []conversation.Citation, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil, fmt.Errorf("query is required")
	}
	var conv *conversation.Conversation
	if conversationID == "" {
		if err := s.ensureKnowledgeBase(ctx, kbID); err != nil {
			return "", nil, err
		}
	}
	var err error
	conv, err = s.conv.Ensure(ctx, kbID, conversationID, query)
	if err != nil {
		return "", nil, err
	}
	history, err := s.conv.RecentMessages(ctx, conv.ID, 10)
	if err != nil {
		return conv.ID, nil, err
	}
	rewritten := s.rewrite(ctx, query, history)
	citations, err := s.retrieve(ctx, kbID, query, rewritten)
	if err != nil {
		return conv.ID, nil, err
	}
	if err := s.conv.Append(ctx, conv.ID, "user", query, nil); err != nil {
		return conv.ID, nil, err
	}

	messages := buildAnswerMessages(history, query, citations)
	var answer strings.Builder
	err = s.chatModel().Stream(ctx, messages, func(token string) error {
		answer.WriteString(token)
		return onToken(token)
	})
	if err != nil {
		return conv.ID, citations, err
	}
	if err := s.conv.Append(ctx, conv.ID, "assistant", answer.String(), citations); err != nil {
		return conv.ID, citations, err
	}
	return conv.ID, citations, nil
}

func (s *Service) ListConversations(ctx context.Context, kbID string) ([]conversation.Conversation, error) {
	return s.conv.ListByKB(ctx, kbID)
}

func (s *Service) ListMessages(ctx context.Context, conversationID string) ([]conversation.Message, error) {
	return s.conv.ListMessages(ctx, conversationID)
}

func (s *Service) rewrite(ctx context.Context, query string, history []conversation.Message) string {
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
	rewritten, err := s.chatModel().Complete(ctx, []port.ChatMessage{
		{Role: "system", Content: "You rewrite questions for document retrieval."},
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(rewritten)
}

func (s *Service) retrieve(ctx context.Context, kbID, query, rewritten string) ([]conversation.Citation, error) {
	queries := []string{query}
	if rewritten != "" && !strings.EqualFold(rewritten, query) {
		queries = append(queries, rewritten)
	}
	ranked := make([][]port.SearchHit, len(queries))
	for index, searchQuery := range queries {
		var err error
		ranked[index], err = s.search.Search(ctx, s.indexName, port.SearchRequest{
			Query: searchQuery, Filter: "kb_biz_id = '" + escapeFilter(kbID) + "'", Limit: 8, Hybrid: true,
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
			DocumentID: item.hit.DocumentID, Title: item.hit.Title, ChunkID: item.hit.ID,
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
