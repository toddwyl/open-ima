// Package ingest 编排文档摄取用例:登记、解析、分块、索引与删除。
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"open-ima/internal/application/port"
	"open-ima/internal/domain/knowledgebase"
	"open-ima/internal/domain/media"
	"open-ima/internal/pkg/idgen"
)

// 后台任务类型。
const (
	JobParseMedia  = "parse_media"
	JobDeleteMedia = "delete_media"
	JobReconcile   = "reconcile"
)

// Service 是文档摄取用例。
type Service struct {
	docs      *media.MediaService
	kbs       *knowledgebase.KBService
	queue     port.Queue
	store     port.FileStore
	parser    port.Parser
	index     port.Indexer
	chunker   *media.Chunker
	indexName string
}

func NewService(
	docs *media.MediaService, kbs *knowledgebase.KBService, queue port.Queue,
	store port.FileStore, parser port.Parser, index port.Indexer,
	chunker *media.Chunker, indexName string,
) *Service {
	return &Service{
		docs: docs, kbs: kbs, queue: queue, store: store,
		parser: parser, index: index, chunker: chunker, indexName: indexName,
	}
}

// CreateMedia 登记文档并投递解析任务;同内容哈希时返回既有文档。
func (s *Service) CreateMedia(ctx context.Context, kbBizID, title, sourceType, sourceURI, fileType, fileHash string) (string, bool, error) {
	exists, err := s.kbs.Exists(ctx, kbBizID)
	if err != nil {
		return "", false, err
	}
	if !exists {
		return "", false, knowledgebase.ErrNotFound
	}
	id, duplicate, err := s.docs.Create(ctx, kbBizID, title, sourceType, sourceURI, fileType, fileHash)
	if err != nil || duplicate {
		return id, duplicate, err
	}
	if _, err := s.queue.Enqueue(ctx, JobParseMedia, map[string]string{"media_biz_id": id}); err != nil {
		return "", false, err
	}
	return id, false, nil
}

func (s *Service) Get(ctx context.Context, id string) (*media.Media, error) {
	return s.docs.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, kbBizID string) ([]media.Media, error) {
	return s.docs.List(ctx, kbBizID)
}

// RetryMedia 重置失败文档并重新投递解析任务。
func (s *Service) RetryMedia(ctx context.Context, id string) error {
	if err := s.docs.Retry(ctx, id); err != nil {
		return err
	}
	_, err := s.queue.Enqueue(ctx, JobParseMedia, map[string]string{"media_biz_id": id})
	return err
}

// DeleteMedia 标记删除并投递清理任务。
func (s *Service) DeleteMedia(ctx context.Context, id string) error {
	if err := s.docs.BeginDelete(ctx, id); err != nil {
		return err
	}
	_, err := s.queue.Enqueue(ctx, JobDeleteMedia, map[string]string{"media_biz_id": id})
	return err
}

// EnqueueReconcile 投递 reconcile 任务,清理卡在 deleting 的文档。
func (s *Service) EnqueueReconcile(ctx context.Context) error {
	_, err := s.queue.Enqueue(ctx, JobReconcile, map[string]string{})
	return err
}

// RegisterHandlers 将任务处理器注册到 queue worker。
func (s *Service) RegisterHandlers(registrar port.JobRegistrar) {
	registrar.RegisterPort(JobParseMedia, s.HandleParseMedia)
	registrar.RegisterPort(JobDeleteMedia, s.HandleDeleteMedia)
	registrar.RegisterPort(JobReconcile, s.HandleReconcile)
}

type mediaPayload struct {
	MediaBizID string `json:"media_biz_id"`
}

// HandleParseMedia 执行 解析→分块→索引 流水线。
func (s *Service) HandleParseMedia(ctx context.Context, job *port.Job) error {
	var payload mediaPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil || payload.MediaBizID == "" {
		return port.Permanent(fmt.Errorf("bad payload: %w", err))
	}
	doc, err := s.docs.Get(ctx, payload.MediaBizID)
	if err != nil {
		return port.Permanent(fmt.Errorf("document %s: %w", payload.MediaBizID, err))
	}
	if doc.Status == media.StatusReady || doc.Status == media.StatusDeleting {
		return nil
	}

	fail := func(stage string, cause error) error {
		var fatal *port.FatalError
		if errors.As(cause, &fatal) {
			if err := s.docs.MarkFailed(ctx, doc.BizID, cause); err != nil {
				return err
			}
			return nil
		}
		if job.RetryCount+1 >= s.queue.MaxRetries() {
			if err := s.docs.MarkFailed(ctx, doc.BizID, fmt.Errorf("%s: %w", stage, cause)); err != nil {
				return err
			}
		}
		return cause
	}

	if err := s.docs.SetStatus(ctx, doc.BizID, media.StatusParsing); err != nil {
		return err
	}
	parsed, err := s.parser.Parse(ctx, s.store.URL(doc.SourceURI), doc.FileType)
	if err != nil {
		return fail(media.StatusParsing, err)
	}

	if err := s.docs.SetStatus(ctx, doc.BizID, media.StatusChunking); err != nil {
		return err
	}
	blocks := make([]media.Block, len(parsed.Blocks))
	for index, block := range parsed.Blocks {
		blocks[index] = media.Block{Type: block.Type, Text: block.Text, Level: block.Level}
	}
	pieces := s.chunker.Chunk(blocks)
	if len(pieces) == 0 {
		return fail(media.StatusChunking, &port.FatalError{Message: "no content chunks produced"})
	}
	stored := make([]media.StoredChunk, len(pieces))
	for index, piece := range pieces {
		stored[index] = media.StoredChunk{
			BizID: idgen.New(), Seq: index, TokenCount: len([]rune(piece.Content)),
		}
	}
	if err := s.docs.ReplaceChunks(ctx, doc.BizID, stored); err != nil {
		return err
	}

	if err := s.docs.SetStatus(ctx, doc.BizID, media.StatusIndexing); err != nil {
		return err
	}
	if err := s.index.DeleteByFilter(ctx, s.indexName, mediaFilter(doc.BizID)); err != nil {
		return fail(media.StatusIndexing, err)
	}
	chunkDocs := make([]port.ChunkDoc, len(pieces))
	for index, piece := range pieces {
		chunkDocs[index] = port.ChunkDoc{
			ID: stored[index].BizID, KBBizID: doc.KBBizID, MediaBizID: doc.BizID,
			Title: doc.Title, Content: piece.RetrievalContent(),
		}
	}
	if err := s.index.AddDocuments(ctx, s.indexName, chunkDocs); err != nil {
		return fail(media.StatusIndexing, err)
	}
	return s.docs.MarkReady(ctx, doc.BizID, len(pieces))
}

// HandleDeleteMedia 清理索引与源文件后移除文档。
func (s *Service) HandleDeleteMedia(ctx context.Context, job *port.Job) error {
	var payload mediaPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil || payload.MediaBizID == "" {
		return port.Permanent(fmt.Errorf("bad payload: %w", err))
	}
	doc, err := s.docs.Get(ctx, payload.MediaBizID)
	if errors.Is(err, media.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.index.DeleteByFilter(ctx, s.indexName, mediaFilter(doc.BizID)); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, doc.SourceURI); err != nil {
		return err
	}
	return s.docs.FinalizeDelete(ctx, doc.BizID)
}

// HandleReconcile 为所有卡在 deleting 的文档重新投递删除任务。
func (s *Service) HandleReconcile(ctx context.Context, _ *port.Job) error {
	ids, err := s.docs.DeletingIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.queue.Enqueue(ctx, JobDeleteMedia, map[string]string{"media_biz_id": id}); err != nil {
			return err
		}
	}
	return nil
}

func mediaFilter(mediaBizID string) string {
	return fmt.Sprintf("media_biz_id = '%s'", mediaBizID)
}
