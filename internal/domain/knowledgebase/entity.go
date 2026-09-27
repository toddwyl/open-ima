// Package knowledgebase 承载知识库聚合:实体、仓储契约与领域服务。
package knowledgebase

import "time"

// KnowledgeBase 是知识库聚合根。实体仅携带业务键;自增主键留在 DB 层。
type KnowledgeBase struct {
	BizID       string    `json:"biz_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DocCount    int       `json:"doc_count"`
	CreatedAt   time.Time `json:"created_at"`
}
