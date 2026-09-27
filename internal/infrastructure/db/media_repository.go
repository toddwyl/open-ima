package db

import (
	"context"
	"database/sql"
	"errors"

	"open-ima/internal/domain/media"
	"open-ima/internal/infrastructure/db/dao"
)

// MediaRepository 是 media.MediaRepository 的 SQL 实现:
// 行级操作委托给 DAO,此处负责实体映射与错误翻译。
type MediaRepository struct{ dao *dao.MediaDAO }

func NewMediaRepository(db *sql.DB) *MediaRepository {
	return &MediaRepository{dao: dao.NewMediaDAO(db)}
}

var _ media.MediaRepository = (*MediaRepository)(nil)

func mediaToEntity(row *dao.MediaRow) *media.Media {
	return &media.Media{
		ID:         row.ID,
		BizID:      row.BizID,
		KBBizID:    row.KBBizID,
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

func (r *MediaRepository) Insert(ctx context.Context, doc *media.Media) error {
	id, err := r.dao.Insert(ctx, dao.MediaRow{
		BizID:      doc.BizID,
		KBBizID:    doc.KBBizID,
		Title:      doc.Title,
		SourceType: doc.SourceType,
		SourceURI:  doc.SourceURI,
		FileType:   doc.FileType,
		FileHash:   doc.FileHash,
	})
	if err != nil {
		return err
	}
	doc.ID = id
	return nil
}

func (r *MediaRepository) FindIDByHash(ctx context.Context, kbBizID, fileHash string) (string, error) {
	existing, err := r.dao.FindIDByHash(ctx, kbBizID, fileHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return existing, err
}

func (r *MediaRepository) Get(ctx context.Context, id string) (*media.Media, error) {
	row, err := r.dao.Get(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, media.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return mediaToEntity(row), nil
}

func (r *MediaRepository) List(ctx context.Context, kbBizID string) ([]media.Media, error) {
	rows, err := r.dao.List(ctx, kbBizID)
	if err != nil {
		return nil, err
	}
	medias := make([]media.Media, 0, len(rows))
	for _, row := range rows {
		row := row
		medias = append(medias, *mediaToEntity(&row))
	}
	return medias, nil
}

func (r *MediaRepository) SetStatus(ctx context.Context, id, status string) error {
	return r.dao.SetStatus(ctx, id, status)
}

func (r *MediaRepository) MarkFailed(ctx context.Context, id, cause string) error {
	return r.dao.MarkFailed(ctx, id, media.StatusFailed, cause)
}

func (r *MediaRepository) ResetFailed(ctx context.Context, id string) (bool, error) {
	return r.dao.ResetFailed(ctx, id, media.StatusPending, media.StatusFailed)
}

func (r *MediaRepository) MarkDeleting(ctx context.Context, id string) (bool, error) {
	return r.dao.MarkDeleting(ctx, id, media.StatusDeleting)
}

func (r *MediaRepository) DeleteChunks(ctx context.Context, mediaBizID string) error {
	return r.dao.DeleteChunks(ctx, mediaBizID)
}

func (r *MediaRepository) ListChunks(ctx context.Context, mediaBizID string) ([]media.StoredChunk, error) {
	rows, err := r.dao.ListChunks(ctx, mediaBizID)
	if err != nil {
		return nil, err
	}
	chunks := make([]media.StoredChunk, 0, len(rows))
	for _, row := range rows {
		chunks = append(chunks, media.StoredChunk{
			BizID: row.BizID, Seq: row.Seq, TokenCount: row.TokenCount,
		})
	}
	return chunks, nil
}

func (r *MediaRepository) ReplaceChunks(ctx context.Context, mediaBizID string, chunks []media.StoredChunk) error {
	rows := make([]dao.ChunkRow, 0, len(chunks))
	for _, chunk := range chunks {
		rows = append(rows, dao.ChunkRow{
			BizID:      chunk.BizID,
			Seq:        chunk.Seq,
			TokenCount: chunk.TokenCount,
		})
	}
	err := r.dao.ReplaceChunks(ctx, mediaBizID, rows)
	if errors.Is(err, sql.ErrNoRows) {
		return media.ErrNotFound
	}
	return err
}

func (r *MediaRepository) MarkReady(ctx context.Context, id string, chunkCount int) error {
	return r.dao.MarkReady(ctx, id, media.StatusReady, chunkCount)
}

func (r *MediaRepository) Delete(ctx context.Context, id string) error {
	return r.dao.Delete(ctx, id)
}

func (r *MediaRepository) DeletingIDs(ctx context.Context) ([]string, error) {
	return r.dao.DeletingIDs(ctx, media.StatusDeleting)
}
