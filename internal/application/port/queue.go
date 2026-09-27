// Package port 定义应用层依赖的外部能力端口。接口类型由 application 的用例消费,
// 由 infrastructure 的技术实现满足;端口只允许依赖标准库。
package port

import (
	"context"
	"encoding/json"
)

// Job 是投递给任务处理器的后台任务。
type Job struct {
	Payload    json.RawMessage
	RetryCount int
}

// JobHandler 处理一类后台任务;返回 PermanentError 包装的错误表示不再重试。
type JobHandler func(ctx context.Context, job *Job) error

// PermanentError 标记不可重试的任务失败。
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }

func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent 将 err 包装为不可重试错误。
func Permanent(err error) *PermanentError { return &PermanentError{Err: err} }

// Queue 是后台任务队列的写入端口。
type Queue interface {
	Enqueue(ctx context.Context, jobType string, payload any) (string, error)
	MaxRetries() int
}

// JobRegistrar 注册任务处理器,由 queue worker 实现。
type JobRegistrar interface {
	RegisterPort(jobType string, handler JobHandler)
}
