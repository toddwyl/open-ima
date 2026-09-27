package db

import (
	"context"
	"database/sql"
	"errors"

	"open-ima/internal/domain/document"
	"open-ima/internal/infrastructure/db/dao"
)

// DocumentRepository 是 document.DocumentRepository 的 SQL 实现:
// 行级操作委托给 DAO,此处负责实体映射与错误翻译。
type DocumentRepository struct{ dao *dao.DocumentDAO }

func NewDocumentRepository(db *sql.DB) *DocumentRepository {
	return &DocumentRepository{dao: dao.NewDocumentDAO(db)}
}

var _ document.DocumentRepository = (*DocumentRepository)(nil)

func documentToEntity(row *dao.DocumentRow) *document.Document {
	return &document.Document{
		ID:         row.ID,
		KBID:       row.KBID,
		Title:      row.Title,
		SourceType: row.SourceType,
		SourceURI:  row.SourceURI,
		FileType:   row.FileType,
		FileHash:   row.FileHash,
		Status:     row.Status,
		Error:      row.Error,
		ChunkCount: row.ChunkCount,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}

func (r *DocumentRepository) Insert(ctx context.Context, doc *document.Document) error {
	return r.dao.Insert(ctx, dao.DocumentRow{
		ID:         doc.ID,
		KBID:       doc.KBID,
		Title:      doc.Title,
		SourceType: doc.SourceType,
		SourceURI:  doc.SourceURI,
		FileType:   doc.FileType,
		FileHash:   doc.FileHash,
	})
}

func (r *DocumentRepository) FindIDByHash(ctx context.Context, kbID, fileHash string) (string, error) {
	existing, err := r.dao.FindIDByHash(ctx, kbID, fileHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return existing, err
}

func (r *DocumentRepository) Get(ctx context.Context, id string) (*document.Document, error) {
	row, err := r.dao.Get(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, document.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return documentToEntity(row), nil
}

func (r *DocumentRepository) List(ctx context.Context, kbID string) ([]document.Document, error) {
	rows, err := r.dao.List(ctx, kbID)
	if err != nil {
		return nil, err
	}
	documents := make([]document.Document, 0, len(rows))
	for _, row := range rows {
		row := row
		documents = append(documents, *documentToEntity(&row))
	}
	return documents, nil
}

func (r *DocumentRepository) SetStatus(ctx context.Context, id, status string) error {
	return r.dao.SetStatus(ctx, id, status)
}

func (r *DocumentRepository) MarkFailed(ctx context.Context, id, cause string) error {
	return r.dao.MarkFailed(ctx, id, document.StatusFailed, cause)
}

func (r *DocumentRepository) ResetFailed(ctx context.Context, id string) (bool, error) {
	return r.dao.ResetFailed(ctx, id, document.StatusPending, document.StatusFailed)
}

func (r *DocumentRepository) MarkDeleting(ctx context.Context, id string) (bool, error) {
	return r.dao.MarkDeleting(ctx, id, document.StatusDeleting)
}

func (r *DocumentRepository) DeleteChunks(ctx context.Context, documentID string) error {
	return r.dao.DeleteChunks(ctx, documentID)
}

func (r *DocumentRepository) ReplaceChunks(ctx context.Context, documentID string, chunks []document.StoredChunk) error {
	rows := make([]dao.ChunkRow, 0, len(chunks))
	for _, chunk := range chunks {
		rows = append(rows, dao.ChunkRow{
			ID:         chunk.ID,
			Seq:        chunk.Seq,
			TokenCount: chunk.TokenCount,
		})
	}
	err := r.dao.ReplaceChunks(ctx, documentID, rows)
	if errors.Is(err, sql.ErrNoRows) {
		return document.ErrNotFound
	}
	return err
}

func (r *DocumentRepository) MarkReady(ctx context.Context, id string, chunkCount int) error {
	return r.dao.MarkReady(ctx, id, document.StatusReady, chunkCount)
}

func (r *DocumentRepository) Delete(ctx context.Context, id string) error {
	return r.dao.Delete(ctx, id)
}

func (r *DocumentRepository) DeletingIDs(ctx context.Context) ([]string, error) {
	return r.dao.DeletingIDs(ctx, document.StatusDeleting)
}
