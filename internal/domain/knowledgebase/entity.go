// Package knowledgebase 承载知识库聚合:实体、仓储契约与领域服务。
package knowledgebase

import "time"

// KnowledgeBase 是知识库聚合根。ID 是数据库主键,BizID 是业务标识。
type KnowledgeBase struct {
	ID          int64     `json:"id"`
	BizID       string    `json:"biz_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DocCount    int       `json:"doc_count"`
	CreatedAt   time.Time `json:"created_at"`
}
