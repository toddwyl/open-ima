package knowledgebase

import "context"

// Repository 是知识库聚合的持久化契约,仅定义接口,实现位于 infrastructure。
type KBRepository interface {
	Insert(ctx context.Context, kb *KnowledgeBase) error
	Exists(ctx context.Context, id string) (bool, error)
	// List 返回全部知识库,DocCount 为各库文档数。
	List(ctx context.Context) ([]KnowledgeBase, error)
	Delete(ctx context.Context, id string) error
}
