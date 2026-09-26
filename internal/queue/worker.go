package queue

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

type Handler func(ctx context.Context, job *Job) error

type Worker struct {
	q            *Queue
	handlers     map[string]Handler
	PollInterval time.Duration
	StaleAfter   time.Duration
}

func NewWorker(q *Queue) *Worker {
	return &Worker{
		q:            q,
		handlers:     map[string]Handler{},
		PollInterval: time.Second,
		StaleAfter:   10 * time.Minute,
	}
}

func (w *Worker) Register(jobType string, h Handler) {
	w.handlers[jobType] = h
}

func (w *Worker) RunOnce(ctx context.Context) bool {
	job, err := w.q.Claim(ctx)
	if err != nil || job == nil {
		return false
	}
	w.handle(ctx, job)
	return true
}

func (w *Worker) handle(ctx context.Context, job *Job) {
	h, ok := w.handlers[job.Type]
	if !ok {
		_ = w.q.FailPermanent(ctx, job.ID)
		return
	}
	err := h(ctx, job)
	var pe *PermanentError
	switch {
	case err == nil:
		_ = w.q.Done(ctx, job.ID)
	case errors.As(err, &pe):
		_ = w.q.FailPermanent(ctx, job.ID)
	default:
		_ = w.q.Fail(ctx, job.ID)
	}
}

func (w *Worker) Start(ctx context.Context, concurrency int) {
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stale := time.NewTicker(w.StaleAfter / 2)
			defer stale.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-stale.C:
					if _, err := w.q.ResetStale(ctx, w.StaleAfter); err != nil {
						log.Printf("queue: reset stale: %v", err)
					}
				default:
				}
				if !w.RunOnce(ctx) {
					select {
					case <-ctx.Done():
						return
					case <-time.After(w.PollInterval):
					}
				}
			}
		}()
	}
	wg.Wait()
}
