// Package kb provides knowledge-base CRUD and URL ingestion.
package kb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"open-ima/internal/media"
	"open-ima/internal/storage"
)

var ErrNameTaken = errors.New("knowledge base name already taken")
var ErrInvalidURL = errors.New("invalid url")
var ErrNotFound = errors.New("knowledge base not found")

type KB struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DocCount    int       `json:"doc_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type Service struct {
	db    *sql.DB
	media *media.Service
	store storage.Storage
	hc    *http.Client
}

func NewService(database *sql.DB, mediaService *media.Service, store storage.Storage) *Service {
	return &Service{
		db: database, media: mediaService, store: store,
		hc: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Service) Create(ctx context.Context, name, description string) (*KB, error) {
	knowledgeBase := &KB{ID: uuid.NewString(), Name: name, Description: description, CreatedAt: time.Now()}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO knowledge_bases (id, name, description) VALUES (?, ?, ?)`,
		knowledgeBase.ID, knowledgeBase.Name, knowledgeBase.Description)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrNameTaken
		}
		return nil, err
	}
	return knowledgeBase, nil
}

func (s *Service) List(ctx context.Context) ([]KB, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT k.id, k.name, k.description, k.created_at,
		       (SELECT COUNT(*) FROM documents d WHERE d.kb_id = k.id) AS doc_count
		FROM knowledge_bases k ORDER BY k.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	knowledgeBases := make([]KB, 0)
	for rows.Next() {
		var knowledgeBase KB
		if err := rows.Scan(
			&knowledgeBase.ID, &knowledgeBase.Name, &knowledgeBase.Description,
			&knowledgeBase.CreatedAt, &knowledgeBase.DocCount,
		); err != nil {
			return nil, err
		}
		knowledgeBases = append(knowledgeBases, knowledgeBase)
	}
	return knowledgeBases, rows.Err()
}

func (s *Service) Delete(ctx context.Context, id string) error {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_bases WHERE id = ?)`, id).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}
	documents, err := s.media.List(ctx, id)
	if err != nil {
		return err
	}
	for _, document := range documents {
		if document.Status == media.StatusDeleting {
			continue
		}
		if err := s.media.Delete(ctx, document.ID); err != nil {
			return fmt.Errorf("delete document %s: %w", document.ID, err)
		}
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM messages WHERE conversation_id IN (SELECT id FROM conversations WHERE kb_id = ?)`, id); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM conversations WHERE kb_id = ?`, id); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM knowledge_bases WHERE id = ?`, id)
	return err
}

const maxURLBytes = 10 << 20

func (s *Service) IngestURL(ctx context.Context, kbID, rawURL string) (string, bool, error) {
	parsedURL, err := url.ParseRequestURI(rawURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return "", false, ErrInvalidURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", "open-ima/1.0")
	resp, err := s.hc.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("fetch %s: status %d", rawURL, resp.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, maxURLBytes+1))
	if err != nil {
		return "", false, err
	}
	if len(content) > maxURLBytes {
		return "", false, fmt.Errorf("fetch %s: page exceeds 10MB limit", rawURL)
	}
	sum := sha256.Sum256(content)
	key := hex.EncodeToString(sum[:])
	if err := s.store.Put(ctx, key, bytes.NewReader(content)); err != nil {
		return "", false, err
	}
	return s.media.CreateDocument(ctx, kbID, titleFromURL(parsedURL), "url", key, "html", key)
}

func titleFromURL(parsedURL *url.URL) string {
	parts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
	if last := parts[len(parts)-1]; last != "" {
		if decoded, err := url.PathUnescape(last); err == nil {
			return decoded
		}
		return last
	}
	return parsedURL.Host
}
