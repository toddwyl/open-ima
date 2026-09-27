// Package rag implements retrieval, chat orchestration, and conversation history.
package rag

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"open-ima/internal/infrastructure/llm"
	"open-ima/internal/infrastructure/meili"
)

type Deps struct {
	DB         *sql.DB
	Meili      *meili.Client
	Chat       *llm.ChatClient
	MeiliIndex string
}

type Service struct {
	deps Deps
	mu   sync.RWMutex
}

var ErrKnowledgeBaseNotFound = errors.New("knowledge base not found")

func NewService(deps Deps) *Service { return &Service{deps: deps} }

func (s *Service) SetChatClient(client *llm.ChatClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deps.Chat = client
}

func (s *Service) chatClient() *llm.ChatClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.deps.Chat
}

type SearchResult struct {
	ChunkID    string  `json:"chunk_id"`
	DocumentID string  `json:"document_id"`
	Title      string  `json:"title"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score"`
}

type Citation struct {
	DocumentID string  `json:"document_id"`
	Title      string  `json:"title"`
	ChunkID    string  `json:"chunk_id"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score"`
}

type Conversation struct {
	ID        string    `json:"id"`
	KBID      string    `json:"kb_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

type Message struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation_id"`
	Role           string     `json:"role"`
	Content        string     `json:"content"`
	Citations      []Citation `json:"citations"`
	CreatedAt      time.Time  `json:"created_at"`
}

func (s *Service) Search(ctx context.Context, kbID, query, mode string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if err := s.ensureKnowledgeBase(ctx, kbID); err != nil {
		return nil, err
	}
	request := meili.SearchRequest{
		Query: query, Filter: "kb_id = '" + escapeFilter(kbID) + "'", Limit: 8,
	}
	switch mode {
	case "", "hybrid":
		request.Hybrid = true
	case "text":
	default:
		return nil, fmt.Errorf("unsupported search mode %q", mode)
	}
	hits, err := s.deps.Meili.Search(ctx, s.deps.MeiliIndex, request)
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
	var exists int
	if err := s.deps.DB.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM knowledge_bases WHERE id = ?)`, kbID,
	).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrKnowledgeBaseNotFound
	}
	return nil
}

func (s *Service) Chat(ctx context.Context, kbID, conversationID, query string, onToken func(string) error) (string, []Citation, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil, fmt.Errorf("query is required")
	}
	conversation, err := s.ensureConversation(ctx, kbID, conversationID, query)
	if err != nil {
		return "", nil, err
	}
	history, err := s.recentMessages(ctx, conversation.ID, 10)
	if err != nil {
		return conversation.ID, nil, err
	}
	rewritten := s.rewrite(ctx, query, history)
	citations, err := s.retrieve(ctx, kbID, query, rewritten)
	if err != nil {
		return conversation.ID, nil, err
	}
	if err := s.insertMessage(ctx, conversation.ID, "user", query, nil); err != nil {
		return conversation.ID, nil, err
	}

	messages := buildAnswerMessages(history, query, citations)
	var answer strings.Builder
	err = s.chatClient().Stream(ctx, messages, func(token string) error {
		answer.WriteString(token)
		return onToken(token)
	})
	if err != nil {
		return conversation.ID, citations, err
	}
	if err := s.insertMessage(ctx, conversation.ID, "assistant", answer.String(), citations); err != nil {
		return conversation.ID, citations, err
	}
	return conversation.ID, citations, nil
}

func (s *Service) ListConversations(ctx context.Context, kbID string) ([]Conversation, error) {
	rows, err := s.deps.DB.QueryContext(ctx,
		`SELECT id, kb_id, title, created_at FROM conversations WHERE kb_id = ? ORDER BY created_at DESC`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	conversations := make([]Conversation, 0)
	for rows.Next() {
		var conversation Conversation
		if err := rows.Scan(&conversation.ID, &conversation.KBID, &conversation.Title, &conversation.CreatedAt); err != nil {
			return nil, err
		}
		conversations = append(conversations, conversation)
	}
	return conversations, rows.Err()
}

func (s *Service) ListMessages(ctx context.Context, conversationID string) ([]Message, error) {
	rows, err := s.deps.DB.QueryContext(ctx,
		`SELECT id, conversation_id, role, content, citations, created_at FROM messages WHERE conversation_id = ? ORDER BY created_at, rowid`,
		conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (s *Service) ensureConversation(ctx context.Context, kbID, conversationID, query string) (*Conversation, error) {
	if conversationID != "" {
		var conversation Conversation
		err := s.deps.DB.QueryRowContext(ctx,
			`SELECT id, kb_id, title, created_at FROM conversations WHERE id = ? AND kb_id = ?`,
			conversationID, kbID,
		).Scan(&conversation.ID, &conversation.KBID, &conversation.Title, &conversation.CreatedAt)
		return &conversation, err
	}
	var exists int
	if err := s.deps.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_bases WHERE id = ?`, kbID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, sql.ErrNoRows
	}
	conversation := &Conversation{ID: uuid.NewString(), KBID: kbID, Title: truncateRunes(query, 80), CreatedAt: time.Now()}
	_, err := s.deps.DB.ExecContext(ctx,
		`INSERT INTO conversations (id, kb_id, title) VALUES (?, ?, ?)`,
		conversation.ID, conversation.KBID, conversation.Title)
	return conversation, err
}

func (s *Service) recentMessages(ctx context.Context, conversationID string, limit int) ([]Message, error) {
	rows, err := s.deps.DB.QueryContext(ctx,
		`SELECT id, conversation_id, role, content, citations, created_at FROM messages WHERE conversation_id = ? ORDER BY created_at DESC, rowid DESC LIMIT ?`,
		conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []Message
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	return messages, nil
}

type rowScanner interface{ Scan(...any) error }

func scanMessage(row rowScanner) (Message, error) {
	var message Message
	var citationsJSON string
	err := row.Scan(
		&message.ID, &message.ConversationID, &message.Role, &message.Content,
		&citationsJSON, &message.CreatedAt,
	)
	if err != nil {
		return Message{}, err
	}
	if err := json.Unmarshal([]byte(citationsJSON), &message.Citations); err != nil {
		return Message{}, fmt.Errorf("decode citations: %w", err)
	}
	return message, nil
}

func (s *Service) insertMessage(ctx context.Context, conversationID, role, content string, citations []Citation) error {
	if citations == nil {
		citations = []Citation{}
	}
	encoded, err := json.Marshal(citations)
	if err != nil {
		return err
	}
	_, err = s.deps.DB.ExecContext(ctx,
		`INSERT INTO messages (id, conversation_id, role, content, citations) VALUES (?, ?, ?, ?, ?)`,
		uuid.NewString(), conversationID, role, content, string(encoded))
	return err
}

func (s *Service) rewrite(ctx context.Context, query string, history []Message) string {
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
	rewritten, err := s.chatClient().Complete(ctx, []llm.Message{
		{Role: "system", Content: "You rewrite questions for document retrieval."},
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(rewritten)
}

func (s *Service) retrieve(ctx context.Context, kbID, query, rewritten string) ([]Citation, error) {
	queries := []string{query}
	if rewritten != "" && !strings.EqualFold(rewritten, query) {
		queries = append(queries, rewritten)
	}
	ranked := make([][]meili.SearchHit, len(queries))
	for index, searchQuery := range queries {
		var err error
		ranked[index], err = s.deps.Meili.Search(ctx, s.deps.MeiliIndex, meili.SearchRequest{
			Query: searchQuery, Filter: "kb_id = '" + escapeFilter(kbID) + "'", Limit: 8, Hybrid: true,
		})
		if err != nil {
			return nil, err
		}
	}
	return fuse(ranked, 8), nil
}

func fuse(rankings [][]meili.SearchHit, limit int) []Citation {
	type scored struct {
		hit   meili.SearchHit
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
	citations := make([]Citation, len(items))
	for index, item := range items {
		snippet := item.hit.Formatted
		if snippet == "" {
			snippet = item.hit.Content
		}
		citations[index] = Citation{
			DocumentID: item.hit.DocumentID, Title: item.hit.Title, ChunkID: item.hit.ID,
			Snippet: snippet, Score: item.score,
		}
	}
	return citations
}

func buildAnswerMessages(history []Message, query string, citations []Citation) []llm.Message {
	var sources strings.Builder
	for index, citation := range citations {
		fmt.Fprintf(&sources, "[%d] %s\n%s\n\n", index+1, citation.Title, citation.Snippet)
	}
	messages := []llm.Message{{
		Role:    "system",
		Content: "Answer from the provided sources. Cite factual claims with source numbers like [1]. Say when the sources do not contain the answer.\n\nSources:\n" + sources.String(),
	}}
	start := max(0, len(history)-10)
	for _, message := range history[start:] {
		messages = append(messages, llm.Message{Role: message.Role, Content: message.Content})
	}
	return append(messages, llm.Message{Role: "user", Content: query})
}

func escapeFilter(value string) string { return strings.ReplaceAll(value, "'", "''") }

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
