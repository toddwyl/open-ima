// Package knowledgebase 编排知识库用例:CRUD、级联删除与 URL 摄取。
package knowledgebase

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"open-ima/internal/application/ingest"
	"open-ima/internal/application/port"
	"open-ima/internal/domain/conversation"
	"open-ima/internal/domain/media"
	kbdom "open-ima/internal/domain/knowledgebase"
)

// ErrInvalidURL 表示摄取地址不是合法的 http(s) URL。
var ErrInvalidURL = errors.New("invalid url")

// Service 是知识库用例。
type Service struct {
	kbs     *kbdom.KBService
	docs    *media.MediaService
	conv    *conversation.ConversationService
	ingest  *ingest.Service
	store   port.FileStore
	fetcher port.Fetcher
}

func NewService(
	kbs *kbdom.KBService, docs *media.MediaService, conv *conversation.ConversationService,
	ingestService *ingest.Service, store port.FileStore, fetcher port.Fetcher,
) *Service {
	return &Service{kbs: kbs, docs: docs, conv: conv, ingest: ingestService, store: store, fetcher: fetcher}
}

func (s *Service) Create(ctx context.Context, name, description string) (*kbdom.KnowledgeBase, error) {
	return s.kbs.Create(ctx, name, description)
}

func (s *Service) List(ctx context.Context) ([]kbdom.KnowledgeBase, error) {
	return s.kbs.List(ctx)
}

// Delete 级联删除:先标记并清理全部文档,再删除会话与消息,最后删除知识库。
func (s *Service) Delete(ctx context.Context, id string) error {
	exists, err := s.kbs.Exists(ctx, id)
	if err != nil {
		return err
	}
	if !exists {
		return kbdom.ErrNotFound
	}
	documents, err := s.docs.List(ctx, id)
	if err != nil {
		return err
	}
	for _, doc := range documents {
		if doc.Status == media.StatusDeleting {
			continue
		}
		if err := s.ingest.DeleteMedia(ctx, doc.BizID); err != nil {
			return fmt.Errorf("delete document %s: %w", doc.BizID, err)
		}
	}
	if err := s.conv.DeleteByKB(ctx, id); err != nil {
		return err
	}
	return s.kbs.Delete(ctx, id)
}

// IngestURL 抓取页面,按内容哈希入库并登记为 html 文档。
func (s *Service) IngestURL(ctx context.Context, kbBizID, rawURL string) (string, bool, error) {
	parsedURL, err := url.ParseRequestURI(rawURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return "", false, ErrInvalidURL
	}
	content, err := s.fetcher.Fetch(ctx, rawURL)
	if err != nil {
		return "", false, err
	}
	sum := sha256.Sum256(content)
	key := hex.EncodeToString(sum[:])
	if err := s.store.Put(ctx, key, bytes.NewReader(content)); err != nil {
		return "", false, err
	}
	return s.ingest.CreateMedia(ctx, kbBizID, titleFromURL(parsedURL), "url", key, "html", key)
}

func titleFromURL(parsedURL *url.URL) string {
	parts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
	if last := parts[len(parts)-1]; last != "" {
		if decoded, err := url.PathUnescape(last); err == nil {
			return decoded
		}
		return last
	}
	return parsedURL.Host
}
