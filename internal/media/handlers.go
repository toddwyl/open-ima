package media

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	domaindoc "open-ima/internal/domain/document"
	"open-ima/internal/infrastructure/meili"
	"open-ima/internal/infrastructure/parser"
	"open-ima/internal/infrastructure/queue"
)

type documentPayload struct {
	DocumentID string `json:"document_id"`
}

func (s *Service) HandleParseDocument(ctx context.Context, job *queue.Job) error {
	var payload documentPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil || payload.DocumentID == "" {
		return queue.Permanent(fmt.Errorf("bad payload: %w", err))
	}
	document, err := s.Get(ctx, payload.DocumentID)
	if err != nil {
		return queue.Permanent(fmt.Errorf("document %s: %w", payload.DocumentID, err))
	}
	if document.Status == StatusReady || document.Status == StatusDeleting {
		return nil
	}

	fail := func(stage string, cause error) error {
		var fatal *parser.FatalError
		if errors.As(cause, &fatal) {
			if err := s.markFailed(ctx, document.ID, cause); err != nil {
				return err
			}
			return nil
		}
		if job.RetryCount+1 >= s.deps.Queue.MaxRetries() {
			if err := s.markFailed(ctx, document.ID, fmt.Errorf("%s: %w", stage, cause)); err != nil {
				return err
			}
		}
		return cause
	}

	if err := s.setStatus(ctx, document.ID, StatusParsing); err != nil {
		return err
	}
	parsed, err := s.deps.Parser.Parse(ctx, s.deps.Store.URL(document.SourceURI), document.FileType)
	if err != nil {
		return fail(StatusParsing, err)
	}

	if err := s.setStatus(ctx, document.ID, StatusChunking); err != nil {
		return err
	}
	blocks := make([]domaindoc.Block, len(parsed.Blocks))
	for index, block := range parsed.Blocks {
		blocks[index] = domaindoc.Block{Type: block.Type, Text: block.Text, Level: block.Level}
	}
	pieces := s.deps.Chunker.Chunk(blocks)
	if len(pieces) == 0 {
		return fail(StatusChunking, &parser.FatalError{Message: "no content chunks produced"})
	}
	if _, err := s.deps.DB.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, document.ID); err != nil {
		return err
	}
	chunkIDs := make([]string, len(pieces))
	for index, piece := range pieces {
		chunkIDs[index] = uuid.NewString()
		if _, err := s.deps.DB.ExecContext(ctx,
			`INSERT INTO chunks (id, document_id, kb_id, seq, token_count) VALUES (?, ?, ?, ?, ?)`,
			chunkIDs[index], document.ID, document.KBID, index, len([]rune(piece.Content))); err != nil {
			return err
		}
	}

	if err := s.setStatus(ctx, document.ID, StatusIndexing); err != nil {
		return err
	}
	filter := fmt.Sprintf("document_id = '%s'", document.ID)
	if err := s.deps.Meili.DeleteByFilter(ctx, s.deps.MeiliIndex, filter); err != nil {
		return fail(StatusIndexing, err)
	}
	documents := make([]meili.ChunkDoc, len(pieces))
	for index, piece := range pieces {
		documents[index] = meili.ChunkDoc{
			ID: chunkIDs[index], KBID: document.KBID, DocumentID: document.ID,
			Title: document.Title, Content: piece.RetrievalContent(),
		}
	}
	if err := s.deps.Meili.AddDocuments(ctx, s.deps.MeiliIndex, documents); err != nil {
		return fail(StatusIndexing, err)
	}
	_, err = s.deps.DB.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = '', chunk_count = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		StatusReady, len(pieces), document.ID)
	return err
}

func (s *Service) HandleDeleteDocument(ctx context.Context, job *queue.Job) error {
	var payload documentPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil || payload.DocumentID == "" {
		return queue.Permanent(fmt.Errorf("bad payload: %w", err))
	}
	document, err := s.Get(ctx, payload.DocumentID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.deps.Meili.DeleteByFilter(ctx, s.deps.MeiliIndex,
		fmt.Sprintf("document_id = '%s'", document.ID)); err != nil {
		return err
	}
	if err := s.deps.Store.Delete(ctx, document.SourceURI); err != nil {
		return err
	}
	_, err = s.deps.DB.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, document.ID)
	return err
}

func (s *Service) HandleReconcile(ctx context.Context, _ *queue.Job) error {
	rows, err := s.deps.DB.QueryContext(ctx, `SELECT id FROM documents WHERE status = ?`, StatusDeleting)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.deps.Queue.Enqueue(ctx, JobDeleteDocument, map[string]string{"document_id": id}); err != nil {
			return err
		}
	}
	return nil
}
