package test

import (
	"context"
	"testing"
	"time"

	"github.com/zukreindev/quantaq"
	quantaqRedis "github.com/zukreindev/quantaq/internal/redis"

	"github.com/alicebob/miniredis/v2"
)

type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time { return m.now }

func setup(t *testing.T) (*quantaq.Client, *miniredis.Miniredis, *quantaq.InMemoryCollector, *mockClock) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb, err := quantaqRedis.NewClient(mr.Addr(), "", 0)
	if err != nil {
		t.Fatalf("redis client: %v", err)
	}

	mc := &mockClock{now: time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)}
	col := quantaq.NewInMemoryCollector()

	client := quantaq.NewClient(rdb, quantaq.WithClock(mc), quantaq.WithMetrics(col))
	return client, mr, col, mc
}

// --- Enqueue ---

func TestEnqueue(t *testing.T) {
	client, mr, col, mc := setup(t)
	ctx := context.Background()

	job, err := client.Enqueue(ctx, "email", []byte(`{"to":"a@b.com"}`), quantaq.EnqueueOptions{MaxAttempts: 5})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if job.Queue != "email" {
		t.Errorf("queue = %q, want %q", job.Queue, "email")
	}
	if job.Status != quantaq.StatusReady {
		t.Errorf("status = %q, want %q", job.Status, quantaq.StatusReady)
	}
	if job.MaxAttempts != 5 {
		t.Errorf("max_attempts = %d, want 5", job.MaxAttempts)
	}
	if !job.CreatedAt.Equal(mc.now) {
		t.Errorf("created_at = %v, want %v", job.CreatedAt, mc.now)
	}

	// Job ID should be in the waiting list
	waiting, err := mr.List(quantaqRedis.WaitingKey("email"))
	if err != nil {
		t.Fatalf("get waiting list: %v", err)
	}
	if len(waiting) != 1 || waiting[0] != job.ID {
		t.Errorf("waiting list = %v, want [%s]", waiting, job.ID)
	}

	// Metrics
	snap := col.Snapshot()
	if snap["email"].Enqueued != 1 {
		t.Errorf("enqueued metric = %d, want 1", snap["email"].Enqueued)
	}
}

func TestEnqueue_DefaultMaxAttempts(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := context.Background()

	job, err := client.Enqueue(ctx, "q", []byte(`{}`), quantaq.EnqueueOptions{})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if job.MaxAttempts != 3 {
		t.Errorf("default max_attempts = %d, want 3", job.MaxAttempts)
	}
}

func TestEnqueue_EmptyQueue(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := context.Background()

	_, err := client.Enqueue(ctx, "", []byte(`{}`), quantaq.EnqueueOptions{})
	if err == nil {
		t.Fatal("expected error for empty queue name")
	}
}

func TestEnqueue_WithMetadata(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := context.Background()

	meta := map[string]string{"type": "welcome"}
	job, err := client.Enqueue(ctx, "email", []byte(`{}`), quantaq.EnqueueOptions{Metadata: meta})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if job.Meta["type"] != "welcome" {
		t.Errorf("meta[type] = %q, want %q", job.Meta["type"], "welcome")
	}
}

// --- EnqueueBatch ---

func TestEnqueueBatch(t *testing.T) {
	client, mr, col, _ := setup(t)
	ctx := context.Background()

	jobs := []quantaq.Job{
		{Payload: []byte(`{"n":1}`)},
		{Payload: []byte(`{"n":2}`)},
		{Payload: []byte(`{"n":3}`)},
	}

	result, err := client.EnqueueBatch(ctx, "batch", jobs, quantaq.EnqueueOptions{MaxAttempts: 2})
	if err != nil {
		t.Fatalf("enqueue batch: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("result len = %d, want 3", len(result))
	}

	for i, j := range result {
		if j.Queue != "batch" {
			t.Errorf("job[%d].queue = %q, want %q", i, j.Queue, "batch")
		}
		if j.MaxAttempts != 2 {
			t.Errorf("job[%d].max_attempts = %d, want 2", i, j.MaxAttempts)
		}
	}

	waiting, err := mr.List(quantaqRedis.WaitingKey("batch"))
	if err != nil {
		t.Fatalf("get waiting list: %v", err)
	}
	if len(waiting) != 3 {
		t.Errorf("waiting list len = %d, want 3", len(waiting))
	}

	snap := col.Snapshot()
	if snap["batch"].Enqueued != 3 {
		t.Errorf("enqueued metric = %d, want 3", snap["batch"].Enqueued)
	}
}

func TestEnqueueBatch_EmptyJobs(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := context.Background()

	_, err := client.EnqueueBatch(ctx, "q", []quantaq.Job{}, quantaq.EnqueueOptions{})
	if err == nil {
		t.Fatal("expected error for empty jobs")
	}
}

// --- Fetch ---

func TestFetch(t *testing.T) {
	client, _, col, _ := setup(t)
	ctx := context.Background()

	enqueued, _ := client.Enqueue(ctx, "work", []byte(`{"k":"v"}`), quantaq.EnqueueOptions{})

	job, err := client.Fetch(ctx, "work")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	if job == nil {
		t.Fatal("expected job, got nil")
	}
	if job.ID != enqueued.ID {
		t.Errorf("id = %q, want %q", job.ID, enqueued.ID)
	}
	if job.Status != quantaq.StatusLeased {
		t.Errorf("status = %q, want %q", job.Status, quantaq.StatusLeased)
	}
	if job.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", job.Attempts)
	}
	if job.StartedAt == nil {
		t.Error("started_at should be set")
	}

	snap := col.Snapshot()
	if snap["work"].Fetched != 1 {
		t.Errorf("fetched metric = %d, want 1", snap["work"].Fetched)
	}
}

func TestFetch_EmptyQueue(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := context.Background()

	job, err := client.Fetch(ctx, "empty")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if job != nil {
		t.Errorf("expected nil job, got %+v", job)
	}
}

// --- Ack ---

func TestAck(t *testing.T) {
	client, mr, col, _ := setup(t)
	ctx := context.Background()

	enqueued, _ := client.Enqueue(ctx, "ackq", []byte(`{}`), quantaq.EnqueueOptions{})
	client.Fetch(ctx, "ackq")

	err := client.Ack(ctx, "ackq", enqueued.ID)
	if err != nil {
		t.Fatalf("ack: %v", err)
	}

	// Processing list should be empty
	processing, _ := mr.List(quantaqRedis.ProcessingKey("ackq"))
	if len(processing) != 0 {
		t.Errorf("processing list len = %d, want 0", len(processing))
	}

	// Status in Redis should be acked
	status := mr.HGet(quantaqRedis.JobKey(enqueued.ID), "status")
	if status != string(quantaq.StatusAcked) {
		t.Errorf("redis status = %q, want %q", status, quantaq.StatusAcked)
	}

	snap := col.Snapshot()
	if snap["ackq"].Acked != 1 {
		t.Errorf("acked metric = %d, want 1", snap["ackq"].Acked)
	}
}

func TestAck_EmptyJobID(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := context.Background()

	err := client.Ack(ctx, "q", "")
	if err == nil {
		t.Fatal("expected error for empty job ID")
	}
}

// --- Nack ---

func TestNack_Requeue(t *testing.T) {
	client, mr, col, _ := setup(t)
	ctx := context.Background()

	enqueued, _ := client.Enqueue(ctx, "nq", []byte(`{}`), quantaq.EnqueueOptions{MaxAttempts: 3})
	client.Fetch(ctx, "nq")

	err := client.Nack(ctx, "nq", enqueued.ID, "temp error")
	if err != nil {
		t.Fatalf("nack: %v", err)
	}

	// Job should be back in waiting list
	waiting, _ := mr.List(quantaqRedis.WaitingKey("nq"))
	found := false
	for _, id := range waiting {
		if id == enqueued.ID {
			found = true
		}
	}
	if !found {
		t.Error("job not found in waiting list after nack")
	}

	snap := col.Snapshot()
	if snap["nq"].Nacked != 1 {
		t.Errorf("nacked metric = %d, want 1", snap["nq"].Nacked)
	}
}

func TestNack_DLQ(t *testing.T) {
	client, mr, col, _ := setup(t)
	ctx := context.Background()

	// MaxAttempts=1 so after fetch (attempts=1) + nack (attempts=2 >= 1) → DLQ
	enqueued, _ := client.Enqueue(ctx, "dlqq", []byte(`{}`), quantaq.EnqueueOptions{MaxAttempts: 1})
	client.Fetch(ctx, "dlqq")

	err := client.Nack(ctx, "dlqq", enqueued.ID, "fatal error")
	if err != nil {
		t.Fatalf("nack: %v", err)
	}

	// Job should be in failed list
	failed, _ := mr.List(quantaqRedis.FailedKey("dlqq"))
	found := false
	for _, id := range failed {
		if id == enqueued.ID {
			found = true
		}
	}
	if !found {
		t.Error("job not found in failed list after DLQ")
	}

	status := mr.HGet(quantaqRedis.JobKey(enqueued.ID), "status")
	if status != string(quantaq.StatusDLQ) {
		t.Errorf("redis status = %q, want %q", status, quantaq.StatusDLQ)
	}

	snap := col.Snapshot()
	if snap["dlqq"].DLQ != 1 {
		t.Errorf("dlq metric = %d, want 1", snap["dlqq"].DLQ)
	}
}

// --- Cancel ---

func TestCancel(t *testing.T) {
	client, mr, col, _ := setup(t)
	ctx := context.Background()

	enqueued, _ := client.Enqueue(ctx, "cq", []byte(`{}`), quantaq.EnqueueOptions{})

	err := client.Cancel(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}

	status := mr.HGet(quantaqRedis.JobKey(enqueued.ID), "status")
	if status != string(quantaq.StatusCanceled) {
		t.Errorf("status = %q, want %q", status, quantaq.StatusCanceled)
	}

	snap := col.Snapshot()
	if snap[""].Canceled != 1 {
		t.Errorf("canceled metric = %d, want 1", snap[""].Canceled)
	}
}

func TestCancel_NotFound(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := context.Background()

	err := client.Cancel(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent job")
	}
}

func TestCancel_AlreadyAcked(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := context.Background()

	enqueued, _ := client.Enqueue(ctx, "cq2", []byte(`{}`), quantaq.EnqueueOptions{})
	client.Fetch(ctx, "cq2")
	client.Ack(ctx, "cq2", enqueued.ID)

	err := client.Cancel(ctx, enqueued.ID)
	if err == nil {
		t.Fatal("expected error canceling acked job")
	}
}

// --- GetJob ---

func TestGetJob(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := context.Background()

	enqueued, _ := client.Enqueue(ctx, "gq", []byte(`{"key":"val"}`), quantaq.EnqueueOptions{MaxAttempts: 7})

	job, err := client.GetJob(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}

	if job.ID != enqueued.ID {
		t.Errorf("id = %q, want %q", job.ID, enqueued.ID)
	}
	if job.MaxAttempts != 7 {
		t.Errorf("max_attempts = %d, want 7", job.MaxAttempts)
	}
	if string(job.Payload) != `{"key":"val"}` {
		t.Errorf("payload = %q, want %q", string(job.Payload), `{"key":"val"}`)
	}
}

func TestGetJob_NotFound(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := context.Background()

	_, err := client.GetJob(ctx, "does-not-exist")
	if err == nil {
		t.Fatal("expected error for nonexistent job")
	}
}

// --- QueueStats ---

func TestQueueStats(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := context.Background()

	client.Enqueue(ctx, "sq", []byte(`{}`), quantaq.EnqueueOptions{})
	client.Enqueue(ctx, "sq", []byte(`{}`), quantaq.EnqueueOptions{})

	ready, leased, failed, err := client.QueueStats(ctx, "sq")
	if err != nil {
		t.Fatalf("queue stats: %v", err)
	}

	if ready != 2 {
		t.Errorf("ready = %d, want 2", ready)
	}
	if leased != 0 {
		t.Errorf("leased = %d, want 0", leased)
	}
	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}

	// Fetch one → leased count goes up
	client.Fetch(ctx, "sq")

	ready, leased, _, err = client.QueueStats(ctx, "sq")
	if err != nil {
		t.Fatalf("queue stats: %v", err)
	}
	if ready != 1 {
		t.Errorf("ready after fetch = %d, want 1", ready)
	}
	if leased != 1 {
		t.Errorf("leased after fetch = %d, want 1", leased)
	}
}

func TestQueueStats_EmptyQueue(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := context.Background()

	_, _, _, err := client.QueueStats(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty queue name")
	}
}

// --- PurgeQueue ---

func TestPurgeQueue(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := context.Background()

	client.Enqueue(ctx, "pq", []byte(`{}`), quantaq.EnqueueOptions{})
	client.Enqueue(ctx, "pq", []byte(`{}`), quantaq.EnqueueOptions{})

	err := client.PurgeQueue(ctx, "pq")
	if err != nil {
		t.Fatalf("purge: %v", err)
	}

	ready, leased, failed, _ := client.QueueStats(ctx, "pq")
	if ready != 0 || leased != 0 || failed != 0 {
		t.Errorf("after purge: ready=%d leased=%d failed=%d, want all 0", ready, leased, failed)
	}
}

// --- Clock Integration ---

func TestClock_UsedInEnqueue(t *testing.T) {
	client, _, _, mc := setup(t)
	ctx := context.Background()

	fixedTime := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	mc.now = fixedTime

	job, _ := client.Enqueue(ctx, "cq", []byte(`{}`), quantaq.EnqueueOptions{})
	if !job.CreatedAt.Equal(fixedTime) {
		t.Errorf("created_at = %v, want %v", job.CreatedAt, fixedTime)
	}
}

func TestClock_UsedInFetch(t *testing.T) {
	client, _, _, mc := setup(t)
	ctx := context.Background()

	client.Enqueue(ctx, "fq", []byte(`{}`), quantaq.EnqueueOptions{})

	fetchTime := time.Date(2030, 6, 15, 10, 30, 0, 0, time.UTC)
	mc.now = fetchTime

	job, _ := client.Fetch(ctx, "fq")
	if job.StartedAt == nil || !job.StartedAt.Equal(fetchTime) {
		t.Errorf("started_at = %v, want %v", job.StartedAt, fetchTime)
	}
}

// --- Full Lifecycle ---

func TestFullLifecycle_EnqueueFetchAck(t *testing.T) {
	client, _, col, _ := setup(t)
	ctx := context.Background()

	enqueued, err := client.Enqueue(ctx, "lifecycle", []byte(`{"action":"test"}`), quantaq.EnqueueOptions{MaxAttempts: 3})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	fetched, err := client.Fetch(ctx, "lifecycle")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if fetched.ID != enqueued.ID {
		t.Fatalf("fetched wrong job")
	}

	err = client.Ack(ctx, "lifecycle", fetched.ID)
	if err != nil {
		t.Fatalf("ack: %v", err)
	}

	ready, leased, failed, _ := client.QueueStats(ctx, "lifecycle")
	if ready != 0 || leased != 0 || failed != 0 {
		t.Errorf("after ack: ready=%d leased=%d failed=%d", ready, leased, failed)
	}

	snap := col.Snapshot()
	lm := snap["lifecycle"]
	if lm.Enqueued != 1 || lm.Fetched != 1 || lm.Acked != 1 {
		t.Errorf("metrics: enqueued=%d fetched=%d acked=%d", lm.Enqueued, lm.Fetched, lm.Acked)
	}
}

func TestFullLifecycle_EnqueueFetchNackRetryAck(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := context.Background()

	enqueued, _ := client.Enqueue(ctx, "retry", []byte(`{}`), quantaq.EnqueueOptions{MaxAttempts: 5})

	// First attempt → nack (Fetch: attempts=1, Nack: attempts=2)
	job, _ := client.Fetch(ctx, "retry")
	client.Nack(ctx, "retry", job.ID, "err1")

	// Second attempt → nack (Fetch: attempts=3, Nack: attempts=4)
	job, _ = client.Fetch(ctx, "retry")
	if job.ID != enqueued.ID {
		t.Fatal("expected same job on retry")
	}
	client.Nack(ctx, "retry", job.ID, "err2")

	// Third attempt → ack (Fetch: attempts=5)
	job, _ = client.Fetch(ctx, "retry")
	err := client.Ack(ctx, "retry", job.ID)
	if err != nil {
		t.Fatalf("ack on 3rd attempt: %v", err)
	}

	ready, leased, _, _ := client.QueueStats(ctx, "retry")
	if ready != 0 || leased != 0 {
		t.Errorf("after retry+ack: ready=%d leased=%d", ready, leased)
	}
}

// --- NewClient Options ---

func TestNewClient_DefaultOptions(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb, _ := quantaqRedis.NewClient(mr.Addr(), "", 0)

	client := quantaq.NewClient(rdb)
	ctx := context.Background()

	// Verify client works with default options (RealClock + NoopCollector)
	job, err := client.Enqueue(ctx, "default-test", []byte(`{}`), quantaq.EnqueueOptions{})
	if err != nil {
		t.Fatalf("enqueue with defaults: %v", err)
	}
	if job.CreatedAt.IsZero() {
		t.Error("created_at should be set by default clock")
	}
}
