package document

import (
	"context"
	"fmt"

	"open-ima/internal/pkg/idgen"
)

// Service 承载文档生命周期规则:登记查重、失败重试、删除保护。
type DocumentService struct {
	repo DocumentRepository
}

func NewDocumentService(repo DocumentRepository) *DocumentService {
	return &DocumentService{repo: repo}
}

// Create 登记新文档;同库同内容哈希时返回既有文档 ID 与 duplicate=true。
func (s *DocumentService) Create(ctx context.Context, kbBizID, title, sourceType, sourceURI, fileType, fileHash string) (string, bool, error) {
	if fileHash != "" {
		existing, err := s.repo.FindIDByHash(ctx, kbBizID, fileHash)
		if err != nil {
			return "", false, err
		}
		if existing != "" {
			return existing, true, nil
		}
	}
	doc := &Document{
		BizID: idgen.New(), KBBizID: kbBizID, Title: title, SourceType: sourceType,
		SourceURI: sourceURI, FileType: fileType, FileHash: fileHash,
		Status: StatusPending,
	}
	if err := s.repo.Insert(ctx, doc); err != nil {
		return "", false, err
	}
	return doc.BizID, false, nil
}

func (s *DocumentService) Get(ctx context.Context, id string) (*Document, error) {
	return s.repo.Get(ctx, id)
}

// ListChunks 按 seq 升序返回文档的分块定位信息，供阅读视图拼接正文。
func (s *DocumentService) ListChunks(ctx context.Context, documentBizID string) ([]StoredChunk, error) {
	return s.repo.ListChunks(ctx, documentBizID)
}

func (s *DocumentService) List(ctx context.Context, kbBizID string) ([]Document, error) {
	return s.repo.List(ctx, kbBizID)
}

// Retry 仅允许重试 failed 文档;重置状态由调用方决定是否重新投递任务。
func (s *DocumentService) Retry(ctx context.Context, id string) error {
	reset, err := s.repo.ResetFailed(ctx, id)
	if err != nil {
		return err
	}
	if reset {
		return nil
	}
	doc, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: document %s has status %s", ErrNotFailed, id, doc.Status)
}

// BeginDelete 标记文档进入删除中,并立即清理已落库的分块。
func (s *DocumentService) BeginDelete(ctx context.Context, id string) error {
	marked, err := s.repo.MarkDeleting(ctx, id)
	if err != nil {
		return err
	}
	if !marked {
		doc, err := s.repo.Get(ctx, id)
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: document %s", ErrDeleting, doc.BizID)
	}
	return s.repo.DeleteChunks(ctx, id)
}

func (s *DocumentService) SetStatus(ctx context.Context, id, status string) error {
	return s.repo.SetStatus(ctx, id, status)
}

func (s *DocumentService) MarkFailed(ctx context.Context, id string, cause error) error {
	return s.repo.MarkFailed(ctx, id, cause.Error())
}

func (s *DocumentService) ReplaceChunks(ctx context.Context, documentBizID string, chunks []StoredChunk) error {
	return s.repo.ReplaceChunks(ctx, documentBizID, chunks)
}

func (s *DocumentService) MarkReady(ctx context.Context, id string, chunkCount int) error {
	return s.repo.MarkReady(ctx, id, chunkCount)
}

// FinalizeDelete 在索引与文件清理完成后移除文档行。
func (s *DocumentService) FinalizeDelete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

func (s *DocumentService) DeletingIDs(ctx context.Context) ([]string, error) {
	return s.repo.DeletingIDs(ctx)
}
