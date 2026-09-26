package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"open-ima/internal/db"
)

func newTestQueue(t *testing.T) *Queue {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	q := New(d)
	q.Backoff = []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}
	return q
}

func TestEnqueueClaimDone(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	id, err := q.Enqueue(ctx, "parse_document", map[string]string{"document_id": "d1"})
	if err != nil || id == "" {
		t.Fatalf("enqueue: %v id=%q", err, id)
	}
	job, err := q.Claim(ctx)
	if err != nil || job == nil {
		t.Fatalf("claim: %v", err)
	}
	if job.ID != id || job.Type != "parse_document" || job.Status != StatusRunning {
		t.Fatalf("job = %+v", job)
	}
	if string(job.Payload) != `{"document_id":"d1"}` {
		t.Fatalf("payload = %s", job.Payload)
	}
	again, _ := q.Claim(ctx)
	if again != nil {
		t.Fatalf("double claim: %+v", again)
	}
	if err := q.Done(ctx, id); err != nil {
		t.Fatal(err)
	}
	var status string
	_ = q.db.QueryRow(`SELECT status FROM jobs WHERE id=?`, id).Scan(&status)
	if status != StatusDone {
		t.Fatalf("status = %s", status)
	}
}

func TestFailBackoffAndExhaustion(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	base := time.Now()
	q.Now = func() time.Time { return base }
	id, _ := q.Enqueue(ctx, "t", map[string]int{"n": 1})

	mustClaim := func() *Job {
		j, err := q.Claim(ctx)
		if err != nil || j == nil {
			t.Fatalf("claim: %v", err)
		}
		return j
	}
	j := mustClaim()
	_ = q.Fail(ctx, j.ID)
	var runAt time.Time
	var retry int
	_ = q.db.QueryRow(`SELECT retry_count, run_at FROM jobs WHERE id=?`, id).Scan(&retry, &runAt)
	if retry != 1 || !runAt.Equal(base.Add(time.Second)) {
		t.Fatalf("retry=%d runAt=%v", retry, runAt)
	}
	if j, _ := q.Claim(ctx); j != nil {
		t.Fatal("claimed before run_at")
	}
	base = base.Add(time.Second)
	_ = q.Fail(ctx, mustClaim().ID)
	base = base.Add(2 * time.Second)
	_ = q.Fail(ctx, mustClaim().ID)
	var status string
	_ = q.db.QueryRow(`SELECT status, retry_count FROM jobs WHERE id=?`, id).Scan(&status, &retry)
	if status != StatusFailed || retry != 3 {
		t.Fatalf("status=%s retry=%d", status, retry)
	}
}

func TestFailPermanent(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	id, _ := q.Enqueue(ctx, "t", nil)
	j, _ := q.Claim(ctx)
	_ = q.FailPermanent(ctx, j.ID)
	var status string
	_ = q.db.QueryRow(`SELECT status FROM jobs WHERE id=?`, id).Scan(&status)
	if status != StatusFailed {
		t.Fatalf("status = %s", status)
	}
}

func TestResetStale(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	base := time.Now()
	q.Now = func() time.Time { return base }
	id, _ := q.Enqueue(ctx, "t", nil)
	j, _ := q.Claim(ctx)
	base = base.Add(11 * time.Minute)
	n, err := q.ResetStale(ctx, 10*time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("reset: n=%d err=%v", n, err)
	}
	var status string
	_ = q.db.QueryRow(`SELECT status FROM jobs WHERE id=?`, id).Scan(&status)
	if status != StatusPending {
		t.Fatalf("status = %s", status)
	}
	_ = j
}

func TestWorkerRunOnceAndPermanent(t *testing.T) {
	q := newTestQueue(t)
	ctx := context.Background()
	w := NewWorker(q)
	var handled []string
	w.Register("ok", func(_ context.Context, job *Job) error {
		handled = append(handled, job.ID)
		return nil
	})
	w.Register("perm", func(_ context.Context, _ *Job) error {
		return Permanent(errors.New("unparseable"))
	})
	idOK, _ := q.Enqueue(ctx, "ok", nil)
	idPerm, _ := q.Enqueue(ctx, "perm", nil)
	if !w.RunOnce(ctx) || !w.RunOnce(ctx) {
		t.Fatal("RunOnce should process both jobs")
	}
	if len(handled) != 1 || handled[0] != idOK {
		t.Fatalf("handled = %v", handled)
	}
	var status string
	_ = q.db.QueryRow(`SELECT status FROM jobs WHERE id=?`, idPerm).Scan(&status)
	if status != StatusFailed {
		t.Fatalf("permanent job status = %s", status)
	}
	if w.RunOnce(ctx) {
		t.Fatal("no jobs left, RunOnce should be false")
	}
}
