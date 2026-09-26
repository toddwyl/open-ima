// Package media owns document lifecycle state and ingestion jobs.
package media

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"open-ima/internal/chunker"
	"open-ima/internal/llm"
	"open-ima/internal/meili"
	"open-ima/internal/parserclient"
	"open-ima/internal/queue"
	"open-ima/internal/storage"
)

const (
	StatusPending  = "pending"
	StatusParsing  = "parsing"
	StatusChunking = "chunking"
	StatusIndexing = "indexing"
	StatusReady    = "ready"
	StatusFailed   = "failed"
	StatusDeleting = "deleting"

	JobParseDocument  = "parse_document"
	JobDeleteDocument = "delete_document"
	JobReconcile      = "reconcile"
)

type Document struct {
	ID         string    `json:"id"`
	KBID       string    `json:"kb_id"`
	Title      string    `json:"title"`
	SourceType string    `json:"source_type"`
	SourceURI  string    `json:"source_uri"`
	FileType   string    `json:"file_type"`
	FileHash   string    `json:"file_hash"`
	Status     string    `json:"status"`
	Error      string    `json:"error"`
	ChunkCount int       `json:"chunk_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Deps struct {
	DB         *sql.DB
	Store      storage.Storage
	Queue      *queue.Queue
	Parser     *parserclient.Client
	Embedder   *llm.EmbeddingClient
	Meili      *meili.Client
	Chunker    *chunker.Chunker
	MeiliIndex string
}

type Service struct{ deps Deps }

func NewService(deps Deps) *Service { return &Service{deps: deps} }

func (s *Service) RegisterHandlers(worker *queue.Worker) {
	worker.Register(JobParseDocument, s.HandleParseDocument)
	worker.Register(JobDeleteDocument, s.HandleDeleteDocument)
	worker.Register(JobReconcile, s.HandleReconcile)
}

func (s *Service) CreateDocument(ctx context.Context, kbID, title, sourceType, sourceURI, fileType, fileHash string) (string, bool, error) {
	if fileHash != "" {
		var existing string
		err := s.deps.DB.QueryRowContext(ctx,
			`SELECT id FROM documents WHERE kb_id = ? AND file_hash = ?`, kbID, fileHash).Scan(&existing)
		if err == nil {
			return existing, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", false, err
		}
	}

	id := uuid.NewString()
	_, err := s.deps.DB.ExecContext(ctx,
		`INSERT INTO documents (id, kb_id, title, source_type, source_uri, file_type, file_hash) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, kbID, title, sourceType, sourceURI, fileType, fileHash)
	if err != nil {
		return "", false, err
	}
	if _, err := s.deps.Queue.Enqueue(ctx, JobParseDocument, map[string]string{"document_id": id}); err != nil {
		return "", false, err
	}
	return id, false, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Document, error) {
	row := s.deps.DB.QueryRowContext(ctx,
		`SELECT id, kb_id, title, source_type, source_uri, file_type, file_hash, status, error, chunk_count, created_at, updated_at FROM documents WHERE id = ?`, id)
	return scanDocument(row)
}

func (s *Service) List(ctx context.Context, kbID string) ([]Document, error) {
	rows, err := s.deps.DB.QueryContext(ctx,
		`SELECT id, kb_id, title, source_type, source_uri, file_type, file_hash, status, error, chunk_count, created_at, updated_at FROM documents WHERE kb_id = ? ORDER BY created_at DESC`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var documents []Document
	for rows.Next() {
		document, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		documents = append(documents, *document)
	}
	return documents, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanDocument(row scanner) (*Document, error) {
	var document Document
	err := row.Scan(
		&document.ID, &document.KBID, &document.Title, &document.SourceType, &document.SourceURI,
		&document.FileType, &document.FileHash, &document.Status, &document.Error, &document.ChunkCount,
		&document.CreatedAt, &document.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &document, nil
}

func (s *Service) Retry(ctx context.Context, id string) error {
	result, err := s.deps.DB.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = '', updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = ?`,
		StatusPending, id, StatusFailed)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return fmt.Errorf("document %s is not in failed status", id)
	}
	_, err = s.deps.Queue.Enqueue(ctx, JobParseDocument, map[string]string{"document_id": id})
	return err
}

func (s *Service) Delete(ctx context.Context, id string) error {
	result, err := s.deps.DB.ExecContext(ctx,
		`UPDATE documents SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status != ?`,
		StatusDeleting, id, StatusDeleting)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return fmt.Errorf("document %s not found or already deleting", id)
	}
	if _, err := s.deps.DB.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, id); err != nil {
		return err
	}
	_, err = s.deps.Queue.Enqueue(ctx, JobDeleteDocument, map[string]string{"document_id": id})
	return err
}

func (s *Service) EnqueueReconcile(ctx context.Context) error {
	_, err := s.deps.Queue.Enqueue(ctx, JobReconcile, map[string]string{})
	return err
}

func (s *Service) setStatus(ctx context.Context, id, status string) error {
	_, err := s.deps.DB.ExecContext(ctx,
		`UPDATE documents SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, id)
	return err
}

func (s *Service) markFailed(ctx context.Context, id string, cause error) error {
	_, err := s.deps.DB.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		StatusFailed, cause.Error(), id)
	return err
}
