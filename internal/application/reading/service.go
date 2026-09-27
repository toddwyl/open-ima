// Package reading 编排文档阅读用例：正文读取与本地文件打开。
// 正文由检索引擎托管，此处按元数据库的 seq 顺序拼接分块内容。
package reading

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"open-ima/internal/application/port"
	"open-ima/internal/domain/media"
)

// ErrNotIndexed 表示文档还没有可分块的内容（尚未完成索引）。
var ErrNotIndexed = errors.New("media has no indexed content yet")

// ErrIsURL 表示目标文档是网页来源，应直接访问其 URL 而非打开本地文件。
var ErrIsURL = errors.New("media is a url source; open its url instead")

// 系统应用打开的临时副本命名与清理策略：文件名带文档标题便于辨认；
// 打开后保留 viewFileDelay 再删（系统应用是异步读取的，30 秒太快会被「打不开」），
// 进程异常退出留下的残留由启动清扫兜底。
const (
	viewFilePattern = "open-ima-view-*"
	viewFileDelay   = time.Hour
	viewFileMaxAge  = 24 * time.Hour
)

// ChunkContent 是一个分块的正文内容，Seq 保持文档内顺序。
type ChunkContent struct {
	ChunkBizID string `json:"chunk_biz_id"`
	Seq        int    `json:"seq"`
	Content    string `json:"content"`
}

// ContentResult 是文档阅读视图的完整数据。
type ContentResult struct {
	MediaBizID string         `json:"media_biz_id"`
	Title      string         `json:"title"`
	SourceType string         `json:"source_type"`
	SourceURI  string         `json:"source_uri"`
	FileType   string         `json:"file_type"`
	Chunks     []ChunkContent `json:"chunks"`
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
	sweepViewFiles(os.TempDir())
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
// 临时文件以文档标题命名，便于在系统应用的窗口标题中辨认；url 来源返回 ErrIsURL。
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
	temporary, err := os.CreateTemp("", "open-ima-view-"+viewFileBase(doc.Title)+"-*."+extension)
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
	// open 命令立即返回而系统按需读取文件；延后清理，给系统应用留出启动时间。
	delayedRemove(temporaryPath)
	return nil
}

func escapeFilter(value string) string { return strings.ReplaceAll(value, "'", "''") }

// viewFileBase 把文档标题净化为可放入临时文件名的基名：只剔除路径分隔与不安全字符，
// 保留中文等正常字符，便于在系统应用窗口中辨认。
func viewFileBase(title string) string {
	base := strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == ':' || unicode.IsControl(r):
			return '_'
		default:
			return r
		}
	}, title)
	base = strings.TrimLeft(strings.TrimSpace(base), ".")
	if base == "" {
		base = "document"
	}
	runes := []rune(base)
	if len(runes) > 60 {
		base = string(runes[:60])
	}
	return base
}

// delayedRemove 在延迟后清理临时文件；系统默认应用打开文件是异步读取的。
func delayedRemove(path string) {
	time.AfterFunc(viewFileDelay, func() { os.Remove(path) })
}

// sweepViewFiles 清理上次运行残留的过期临时副本（进程异常退出时 delayedRemove 不会触发）。
func sweepViewFiles(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, viewFilePattern))
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-viewFileMaxAge)
	for _, path := range matches {
		info, err := os.Stat(path)
		if err == nil && info.ModTime().Before(cutoff) {
			os.Remove(path)
		}
	}
}
