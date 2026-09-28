// Package ingest 编排文档摄取用例:登记、解析、分块、索引与删除。
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"

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
	indexName string

	chunkMu sync.RWMutex
	chunker *media.Chunker
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

// SetChunker 热切换分块器;仅影响此后执行的入库任务,存量索引需重建才生效。
func (s *Service) SetChunker(chunker *media.Chunker) {
	s.chunkMu.Lock()
	defer s.chunkMu.Unlock()
	s.chunker = chunker
}

func (s *Service) currentChunker() *media.Chunker {
	s.chunkMu.RLock()
	defer s.chunkMu.RUnlock()
	return s.chunker
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

// EnqueueReconcile 投递内部定时对账任务。
func (s *Service) EnqueueReconcile(ctx context.Context) error {
	_, _, err := s.queue.EnqueueUnique(ctx, JobReconcile, "reconcile:light", reconcilePayload{Scope: "light"})
	return err
}

// EnqueueReindex 将全部非 deleting 文档重置为 pending 并重新投递解析任务,
// 使存量文档按当前分块/向量设置重建索引;返回入队文档数。
func (s *Service) EnqueueReindex(ctx context.Context) (int, error) {
	ids, err := s.docs.ReindexableIDs(ctx)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := s.docs.ResetForReindex(ctx, id); err != nil {
			return 0, err
		}
		if _, err := s.queue.Enqueue(ctx, JobParseMedia, map[string]string{"media_biz_id": id}); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
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

type reconcilePayload struct {
	Scope string `json:"scope"`
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
	pieces := s.currentChunker().Chunk(blocks)
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

// HandleReconcile 执行内部定时对账:以 medias 为事实账本,修复外部投影差异。
func (s *Service) HandleReconcile(ctx context.Context, job *port.Job) error {
	scope := "light"
	if job != nil && len(job.Payload) > 0 {
		var payload reconcilePayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return port.Permanent(fmt.Errorf("bad payload: %w", err))
		}
		if payload.Scope != "" {
			scope = payload.Scope
		}
	}
	if scope != "light" {
		return port.Permanent(fmt.Errorf("unsupported reconcile scope %q", scope))
	}
	candidates, err := s.docs.ReconcileCandidates(ctx)
	if err != nil {
		return err
	}
	stats := reconcileStats{scope: scope}
	for _, doc := range candidates {
		stats.scanned++
		if doc.Status == media.StatusDeleting {
			if err := s.enqueueUnique(ctx, JobDeleteMedia, doc.BizID); err != nil {
				stats.failed++
				stats.lastError = err
				return err
			}
			stats.anomalies++
			stats.repaired++
			log.Printf("reconcile action=enqueue_delete anomaly=deleting_stuck media_biz_id=%s reason=%q job_type=%s result=ok",
				doc.BizID, "deleting media has no active delete job", JobDeleteMedia)
			continue
		}
		sourceOK, err := s.reconcileSource(ctx, doc, &stats)
		if err != nil {
			return err
		}
		if !sourceOK {
			continue
		}
		if doc.Status == media.StatusReady {
			if err := s.reconcileIndexedChunks(ctx, doc, &stats); err != nil {
				return err
			}
		}
	}
	log.Printf("reconcile action=summary scope=%s scanned=%d anomalies=%d repaired=%d failed=%d last_error=%q",
		stats.scope, stats.scanned, stats.anomalies, stats.repaired, stats.failed, stats.lastErrorString())
	return nil
}

type reconcileStats struct {
	scope     string
	scanned   int
	anomalies int
	repaired  int
	failed    int
	lastError error
}

func (s *Service) reconcileSource(ctx context.Context, doc media.Media, stats *reconcileStats) (bool, error) {
	inspector, ok := s.store.(port.FileStoreInspector)
	if !ok || doc.SourceURI == "" {
		return true, nil
	}
	exists, err := inspector.Exists(ctx, doc.SourceURI)
	if err != nil {
		stats.failed++
		stats.lastError = err
		return false, err
	}
	if exists {
		return true, nil
	}
	cause := fmt.Errorf("reconcile: storage source missing: %s", doc.SourceURI)
	if err := s.docs.MarkFailed(ctx, doc.BizID, cause); err != nil {
		stats.failed++
		stats.lastError = err
		return false, err
	}
	stats.anomalies++
	stats.failed++
	stats.lastError = cause
	log.Printf("reconcile action=mark_failed anomaly=storage_missing_file media_biz_id=%s resource=storage resource_key=%s reason=%q result=ok",
		doc.BizID, doc.SourceURI, "media source_uri missing in storage")
	return false, nil
}

func (s *Service) reconcileIndexedChunks(ctx context.Context, doc media.Media, stats *reconcileStats) error {
	chunks, err := s.docs.ListChunks(ctx, doc.BizID)
	if err != nil {
		stats.failed++
		stats.lastError = err
		return err
	}
	if len(chunks) == 0 || len(chunks) != doc.ChunkCount {
		return s.enqueueReparse(ctx, doc.BizID, "chunk_count_mismatch", "media chunk_count differs from chunk rows", stats)
	}
	inspector, ok := s.index.(port.IndexInspector)
	if !ok {
		return nil
	}
	indexed, err := inspector.ListByMedia(ctx, s.indexName, doc.BizID, len(chunks)+1)
	if err != nil {
		stats.failed++
		stats.lastError = err
		return err
	}
	if sameChunkIDs(chunks, indexed) {
		return nil
	}
	return s.enqueueReparse(ctx, doc.BizID, "meili_missing_chunk", "indexed chunks differ from media chunks", stats)
}

func (s *Service) enqueueReparse(ctx context.Context, mediaBizID, anomaly, reason string, stats *reconcileStats) error {
	if err := s.docs.ResetForReindex(ctx, mediaBizID); err != nil {
		stats.failed++
		stats.lastError = err
		return err
	}
	if err := s.enqueueUnique(ctx, JobParseMedia, mediaBizID); err != nil {
		stats.failed++
		stats.lastError = err
		return err
	}
	stats.anomalies++
	stats.repaired++
	log.Printf("reconcile action=enqueue_parse anomaly=%s media_biz_id=%s reason=%q job_type=%s result=ok",
		anomaly, mediaBizID, reason, JobParseMedia)
	return nil
}

func (s *Service) enqueueUnique(ctx context.Context, jobType, mediaBizID string) error {
	_, _, err := s.queue.EnqueueUnique(ctx, jobType, jobType+":"+mediaBizID, map[string]string{"media_biz_id": mediaBizID})
	return err
}

func (s *reconcileStats) lastErrorString() string {
	if s.lastError == nil {
		return ""
	}
	return s.lastError.Error()
}

func sameChunkIDs(chunks []media.StoredChunk, indexed []port.IndexedChunkRef) bool {
	if len(chunks) != len(indexed) {
		return false
	}
	want := make(map[string]bool, len(chunks))
	for _, chunk := range chunks {
		want[chunk.BizID] = true
	}
	for _, ref := range indexed {
		if !want[ref.ID] {
			return false
		}
	}
	return true
}

func mediaFilter(mediaBizID string) string {
	return fmt.Sprintf("media_biz_id = '%s'", mediaBizID)
}
