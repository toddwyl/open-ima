// Package queue provides a SQLite-backed in-process job queue.
package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"open-ima/internal/application/port"
)

const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

type Job struct {
	ID         string
	Type       string
	Payload    json.RawMessage
	Status     string
	RetryCount int
	RunAt      time.Time
}

// PermanentError 与 Permanent 是 port 对应类型的别名,任务失败语义统一定义在端口层。
type PermanentError = port.PermanentError

func Permanent(err error) *PermanentError { return port.Permanent(err) }

type Queue struct {
	db      *sql.DB
	Backoff []time.Duration
	Now     func() time.Time
}

func New(db *sql.DB) *Queue {
	return &Queue{
		db:      db,
		Backoff: []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute},
		Now:     time.Now,
	}
}

func (q *Queue) MaxRetries() int {
	return len(q.Backoff)
}

func (q *Queue) Enqueue(ctx context.Context, jobType string, payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	_, err = q.db.ExecContext(ctx,
		`INSERT INTO jobs (id, type, payload, run_at) VALUES (?, ?, ?, ?)`,
		id, jobType, string(data), q.Now().UTC())
	return id, err
}

func (q *Queue) Claim(ctx context.Context) (*Job, error) {
	now := q.Now().UTC()
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var j Job
	var payload string
	err = tx.QueryRowContext(ctx,
		`SELECT id, type, payload, retry_count, run_at FROM jobs
		 WHERE status = ? AND run_at <= ? ORDER BY run_at LIMIT 1`,
		StatusPending, now).Scan(&j.ID, &j.Type, &payload, &j.RetryCount, &j.RunAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	j.Payload = json.RawMessage(payload)
	j.Status = StatusRunning
	j.RunAt = now
	if _, err := tx.ExecContext(ctx,
		`UPDATE jobs SET status = ?, run_at = ? WHERE id = ?`,
		StatusRunning, now, j.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &j, nil
}

func (q *Queue) Done(ctx context.Context, id string) error {
	_, err := q.db.ExecContext(ctx, `UPDATE jobs SET status = ? WHERE id = ?`, StatusDone, id)
	return err
}

func (q *Queue) Fail(ctx context.Context, id string) error {
	var retry int
	if err := q.db.QueryRowContext(ctx, `SELECT retry_count FROM jobs WHERE id = ?`, id).Scan(&retry); err != nil {
		return err
	}
	retry++
	if retry >= q.MaxRetries() {
		_, err := q.db.ExecContext(ctx,
			`UPDATE jobs SET status = ?, retry_count = ? WHERE id = ?`,
			StatusFailed, retry, id)
		return err
	}
	runAt := q.Now().Add(q.Backoff[retry-1]).UTC()
	_, err := q.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, retry_count = ?, run_at = ? WHERE id = ?`,
		StatusPending, retry, runAt, id)
	return err
}

func (q *Queue) FailPermanent(ctx context.Context, id string) error {
	_, err := q.db.ExecContext(ctx, `UPDATE jobs SET status = ? WHERE id = ?`, StatusFailed, id)
	return err
}

func (q *Queue) ResetStale(ctx context.Context, staleAfter time.Duration) (int64, error) {
	cutoff := q.Now().Add(-staleAfter).UTC()
	res, err := q.db.ExecContext(ctx,
		`UPDATE jobs SET status = ? WHERE status = ? AND run_at < ?`,
		StatusPending, StatusRunning, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
