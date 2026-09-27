package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"open-ima/internal/domain/document"
)

// DocumentRepository 是 document.Repository 的 SQLite 实现。
type DocumentRepository struct{ db *sql.DB }

func NewDocumentRepository(db *sql.DB) *DocumentRepository {
	return &DocumentRepository{db: db}
}

var _ document.Repository = (*DocumentRepository)(nil)

func (r *DocumentRepository) Insert(ctx context.Context, doc *document.Document) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO documents (id, kb_id, title, source_type, source_uri, file_type, file_hash) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		doc.ID, doc.KBID, doc.Title, doc.SourceType, doc.SourceURI, doc.FileType, doc.FileHash)
	return err
}

func (r *DocumentRepository) FindIDByHash(ctx context.Context, kbID, fileHash string) (string, error) {
	var existing string
	err := r.db.QueryRowContext(ctx,
		`SELECT id FROM documents WHERE kb_id = ? AND file_hash = ?`, kbID, fileHash).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return existing, err
}

type scanner interface{ Scan(dest ...any) error }

func scanDocument(row scanner) (*document.Document, error) {
	var doc document.Document
	err := row.Scan(
		&doc.ID, &doc.KBID, &doc.Title, &doc.SourceType, &doc.SourceURI,
		&doc.FileType, &doc.FileHash, &doc.Status, &doc.Error, &doc.ChunkCount,
		&doc.CreatedAt, &doc.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, document.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

const documentColumns = `id, kb_id, title, source_type, source_uri, file_type, file_hash, status, error, chunk_count, created_at, updated_at`

func (r *DocumentRepository) Get(ctx context.Context, id string) (*document.Document, error) {
	return scanDocument(r.db.QueryRowContext(ctx,
		`SELECT `+documentColumns+` FROM documents WHERE id = ?`, id))
}

func (r *DocumentRepository) List(ctx context.Context, kbID string) ([]document.Document, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+documentColumns+` FROM documents WHERE kb_id = ? ORDER BY created_at DESC`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	documents := make([]document.Document, 0)
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		documents = append(documents, *doc)
	}
	return documents, rows.Err()
}

func (r *DocumentRepository) SetStatus(ctx context.Context, id, status string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE documents SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, id)
	return err
}

func (r *DocumentRepository) MarkFailed(ctx context.Context, id, cause string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		document.StatusFailed, cause, id)
	return err
}

func (r *DocumentRepository) ResetFailed(ctx context.Context, id string) (bool, error) {
	result, err := r.db.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = '', updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = ?`,
		document.StatusPending, id, document.StatusFailed)
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

func (r *DocumentRepository) MarkDeleting(ctx context.Context, id string) (bool, error) {
	result, err := r.db.ExecContext(ctx,
		`UPDATE documents SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status != ?`,
		document.StatusDeleting, id, document.StatusDeleting)
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

func (r *DocumentRepository) DeleteChunks(ctx context.Context, documentID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, documentID)
	return err
}

func (r *DocumentRepository) ReplaceChunks(ctx context.Context, documentID string, chunks []document.StoredChunk) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, documentID); err != nil {
		return err
	}
	var kbID string
	if err := tx.QueryRowContext(ctx, `SELECT kb_id FROM documents WHERE id = ?`, documentID).Scan(&kbID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return document.ErrNotFound
		}
		return err
	}
	for _, chunk := range chunks {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO chunks (id, document_id, kb_id, seq, token_count) VALUES (?, ?, ?, ?, ?)`,
			chunk.ID, documentID, kbID, chunk.Seq, chunk.TokenCount); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *DocumentRepository) MarkReady(ctx context.Context, id string, chunkCount int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = '', chunk_count = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		document.StatusReady, chunkCount, id)
	return err
}

func (r *DocumentRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, id)
	return err
}

func (r *DocumentRepository) DeletingIDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id FROM documents WHERE status = ?`, document.StatusDeleting)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
