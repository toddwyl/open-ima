// Package knowledgebase 承载知识库聚合:实体、仓储契约与领域服务。
package knowledgebase

import "time"

// KnowledgeBase 是知识库聚合根。
type KnowledgeBase struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DocCount    int       `json:"doc_count"`
	CreatedAt   time.Time `json:"created_at"`
}
