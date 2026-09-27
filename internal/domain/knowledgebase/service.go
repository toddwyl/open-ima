package knowledgebase

import (
	"context"

	"open-ima/internal/domain/idgen"
)

// Service 承载知识库命名唯一性与存在性规则。
type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service { return &Service{repo: repo} }

// Create 创建知识库;重名时返回 ErrNameTaken。
func (s *Service) Create(ctx context.Context, name, description string) (*KnowledgeBase, error) {
	kb := &KnowledgeBase{ID: idgen.New(), Name: name, Description: description}
	if err := s.repo.Insert(ctx, kb); err != nil {
		return nil, err
	}
	return kb, nil
}

func (s *Service) Exists(ctx context.Context, id string) (bool, error) {
	return s.repo.Exists(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]KnowledgeBase, error) {
	return s.repo.List(ctx)
}

// Delete 删除知识库本身;不存在时返回 ErrNotFound。关联数据的级联清理由应用层编排。
func (s *Service) Delete(ctx context.Context, id string) error {
	exists, err := s.repo.Exists(ctx, id)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return s.repo.Delete(ctx, id)
}
