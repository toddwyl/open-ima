package dao

import (
	"context"
	"database/sql"
	"time"
)

// DocumentRow 是 documents 表的一行。
type DocumentRow struct {
	ID         int64
	BizID      string
	KBBizID    string
	Title      string
	SourceType string
	SourceURI  string
	FileType   string
	FileHash   string
	Status     string
	Error      string
	ChunkCount int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ChunkRow 是 chunks 表的一行;内容本体由检索引擎托管,此处仅存定位信息。
type ChunkRow struct {
	BizID      string
	Seq        int
	TokenCount int
}

const documentColumns = `id, document_biz_id, kb_biz_id, title, source_type, source_uri, file_type, file_hash, status, error, chunk_count, created_at, updated_at`

func scanDocument(row scanner) (*DocumentRow, error) {
	var doc DocumentRow
	err := row.Scan(
		&doc.ID, &doc.BizID, &doc.KBBizID, &doc.Title, &doc.SourceType, &doc.SourceURI,
		&doc.FileType, &doc.FileHash, &doc.Status, &doc.Error, &doc.ChunkCount,
		&doc.CreatedAt, &doc.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// DocumentDAO 封装 documents 与 chunks 表的行级操作。状态值由调用方传入,
// DAO 不感知领域生命周期常量。
type DocumentDAO struct{ db *sql.DB }

func NewDocumentDAO(db *sql.DB) *DocumentDAO { return &DocumentDAO{db: db} }

func (d *DocumentDAO) Insert(ctx context.Context, row DocumentRow) (int64, error) {
	result, err := d.db.ExecContext(ctx,
		`INSERT INTO documents (document_biz_id, kb_biz_id, title, source_type, source_uri, file_type, file_hash) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		row.BizID, row.KBBizID, row.Title, row.SourceType, row.SourceURI, row.FileType, row.FileHash)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// FindIDByHash 按内容哈希查重;未命中返回 sql.ErrNoRows。
func (d *DocumentDAO) FindIDByHash(ctx context.Context, kbBizID, fileHash string) (string, error) {
	var existing string
	err := d.db.QueryRowContext(ctx,
		`SELECT document_biz_id FROM documents WHERE kb_biz_id = ? AND file_hash = ?`, kbBizID, fileHash).Scan(&existing)
	return existing, err
}

// Get 未命中返回 sql.ErrNoRows。
func (d *DocumentDAO) Get(ctx context.Context, id string) (*DocumentRow, error) {
	return scanDocument(d.db.QueryRowContext(ctx,
		`SELECT `+documentColumns+` FROM documents WHERE document_biz_id = ?`, id))
}

func (d *DocumentDAO) List(ctx context.Context, kbBizID string) ([]DocumentRow, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT `+documentColumns+` FROM documents WHERE kb_biz_id = ? ORDER BY id DESC`, kbBizID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	documents := make([]DocumentRow, 0)
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		documents = append(documents, *doc)
	}
	return documents, rows.Err()
}

func (d *DocumentDAO) SetStatus(ctx context.Context, id, status string) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE documents SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE document_biz_id = ?`, status, id)
	return err
}

func (d *DocumentDAO) MarkFailed(ctx context.Context, id, failedStatus, cause string) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = ?, updated_at = CURRENT_TIMESTAMP WHERE document_biz_id = ?`,
		failedStatus, cause, id)
	return err
}

// ResetFailed 将 failedStatus 文档重置为 pendingStatus;返回是否有行被更新。
func (d *DocumentDAO) ResetFailed(ctx context.Context, id, pendingStatus, failedStatus string) (bool, error) {
	result, err := d.db.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = '', updated_at = CURRENT_TIMESTAMP WHERE document_biz_id = ? AND status = ?`,
		pendingStatus, id, failedStatus)
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

// MarkDeleting 将非 deletingStatus 文档标记为 deletingStatus;返回是否有行被更新。
func (d *DocumentDAO) MarkDeleting(ctx context.Context, id, deletingStatus string) (bool, error) {
	result, err := d.db.ExecContext(ctx,
		`UPDATE documents SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE document_biz_id = ? AND status != ?`,
		deletingStatus, id, deletingStatus)
	if err != nil {
		return false, err
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

func (d *DocumentDAO) DeleteChunks(ctx context.Context, documentBizID string) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM chunks WHERE document_biz_id = ?`, documentBizID)
	return err
}

// ListChunks 按 seq 升序返回文档的分块定位信息。
func (d *DocumentDAO) ListChunks(ctx context.Context, documentBizID string) ([]ChunkRow, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT chunk_biz_id, seq, token_count FROM chunks WHERE document_biz_id = ? ORDER BY seq`, documentBizID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chunks := make([]ChunkRow, 0)
	for rows.Next() {
		var chunk ChunkRow
		if err := rows.Scan(&chunk.BizID, &chunk.Seq, &chunk.TokenCount); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, rows.Err()
}

// ReplaceChunks 在一个事务里重建文档分块;文档不存在返回 sql.ErrNoRows。
func (d *DocumentDAO) ReplaceChunks(ctx context.Context, documentBizID string, chunks []ChunkRow) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE document_biz_id = ?`, documentBizID); err != nil {
		return err
	}
	var kbBizID string
	if err := tx.QueryRowContext(ctx, `SELECT kb_biz_id FROM documents WHERE document_biz_id = ?`, documentBizID).Scan(&kbBizID); err != nil {
		return err
	}
	for _, chunk := range chunks {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO chunks (chunk_biz_id, document_biz_id, kb_biz_id, seq, token_count) VALUES (?, ?, ?, ?, ?)`,
			chunk.BizID, documentBizID, kbBizID, chunk.Seq, chunk.TokenCount); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DocumentDAO) MarkReady(ctx context.Context, id, readyStatus string, chunkCount int) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = '', chunk_count = ?, updated_at = CURRENT_TIMESTAMP WHERE document_biz_id = ?`,
		readyStatus, chunkCount, id)
	return err
}

func (d *DocumentDAO) Delete(ctx context.Context, id string) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM documents WHERE document_biz_id = ?`, id)
	return err
}

func (d *DocumentDAO) DeletingIDs(ctx context.Context, deletingStatus string) ([]string, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT document_biz_id FROM documents WHERE status = ?`, deletingStatus)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIDs(rows)
}

// ReindexableIDs 返回所有非 deletingStatus 文档,供全量重建索引。
func (d *DocumentDAO) ReindexableIDs(ctx context.Context, deletingStatus string) ([]string, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT document_biz_id FROM documents WHERE status != ?`, deletingStatus)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIDs(rows)
}

// ResetForReindex 将文档重置为 pendingStatus 并清空错误,供重建索引前调用。
func (d *DocumentDAO) ResetForReindex(ctx context.Context, id, pendingStatus string) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE documents SET status = ?, error = '', updated_at = CURRENT_TIMESTAMP WHERE document_biz_id = ?`,
		pendingStatus, id)
	return err
}

func scanIDs(rows *sql.Rows) ([]string, error) {
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
