// Package reading 编排文档阅读用例：正文读取与本地文件打开。
// 正文由检索引擎托管，此处按元数据库的 seq 顺序拼接分块内容。
package reading

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"open-ima/internal/application/port"
	"open-ima/internal/domain/media"
)

// ErrNotIndexed 表示文档还没有可分块的内容（尚未完成索引）。
var ErrNotIndexed = errors.New("media has no indexed content yet")

// ErrIsURL 表示目标文档是网页来源，应直接访问其 URL 而非打开本地文件。
var ErrIsURL = errors.New("media is a url source; open its url instead")

// ChunkContent 是一个分块的正文内容，Seq 保持文档内顺序。
type ChunkContent struct {
	ChunkBizID string `json:"chunk_biz_id"`
	Seq        int    `json:"seq"`
	Content    string `json:"content"`
}

// ContentResult 是文档阅读视图的完整数据。
type ContentResult struct {
	MediaBizID string         `json:"media_biz_id"`
	Title         string         `json:"title"`
	SourceType    string         `json:"source_type"`
	SourceURI     string         `json:"source_uri"`
	FileType      string         `json:"file_type"`
	Chunks        []ChunkContent `json:"chunks"`
}

// Service 是文档阅读用例。
type Service struct {
	docs      *media.MediaService
	search    port.Searcher
	store     port.FileStore
	opener    port.Opener
	indexName string
}

func NewService(docs *media.MediaService, search port.Searcher, store port.FileStore, opener port.Opener, indexName string) *Service {
	return &Service{docs: docs, search: search, store: store, opener: opener, indexName: indexName}
}

// Content 返回按 seq 排序的分块正文；文档不存在返回 media.ErrNotFound。
func (s *Service) Content(ctx context.Context, mediaBizID string) (*ContentResult, error) {
	doc, err := s.docs.Get(ctx, mediaBizID)
	if err != nil {
		return nil, err
	}
	chunks, err := s.docs.ListChunks(ctx, mediaBizID)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotIndexed, mediaBizID)
	}
	hits, err := s.search.Search(ctx, s.indexName, port.SearchRequest{
		Query: "", Filter: "media_biz_id = '" + escapeFilter(mediaBizID) + "'", Limit: len(chunks),
	})
	if err != nil {
		return nil, err
	}
	contentByID := make(map[string]string, len(hits))
	for _, hit := range hits {
		contentByID[hit.ID] = hit.Content
	}
	result := &ContentResult{
		MediaBizID: doc.BizID, Title: doc.Title, SourceType: doc.SourceType,
		SourceURI: doc.SourceURI, FileType: doc.FileType,
		Chunks: make([]ChunkContent, 0, len(chunks)),
	}
	for _, chunk := range chunks {
		result.Chunks = append(result.Chunks, ChunkContent{
			ChunkBizID: chunk.BizID, Seq: chunk.Seq, Content: contentByID[chunk.BizID],
		})
	}
	return result, nil
}

// Open 把文档的存储副本落成一个带扩展名的临时文件并用系统默认应用打开。
// url 来源返回 ErrIsURL；文档不存在返回 media.ErrNotFound。
func (s *Service) Open(ctx context.Context, mediaBizID string) error {
	doc, err := s.docs.Get(ctx, mediaBizID)
	if err != nil {
		return err
	}
	if doc.SourceType != "file" {
		return fmt.Errorf("%w: %s", ErrIsURL, mediaBizID)
	}
	blob, err := s.store.Get(ctx, doc.SourceURI)
	if err != nil {
		return fmt.Errorf("read stored file: %w", err)
	}
	defer blob.Close()
	extension := doc.FileType
	if extension == "" {
		extension = "bin"
	}
	temporary, err := os.CreateTemp("", "open-ima-view-*."+extension)
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	if _, err := io.Copy(temporary, blob); err != nil {
		_ = temporary.Close()
		os.Remove(temporaryPath)
		return err
	}
	if err := temporary.Close(); err != nil {
		os.Remove(temporaryPath)
		return err
	}
	if err := s.opener.Open(ctx, temporaryPath); err != nil {
		os.Remove(temporaryPath)
		return err
	}
	// open 命令立即返回而系统按需读取文件；稍后清理临时副本。
	delayedRemove(temporaryPath)
	return nil
}

func escapeFilter(value string) string { return strings.ReplaceAll(value, "'", "''") }

// delayedRemove 在延迟后清理临时文件；系统默认应用打开文件是异步读取的。
func delayedRemove(path string) {
	time.AfterFunc(30*time.Second, func() { os.Remove(path) })
}
