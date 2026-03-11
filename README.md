<p align="center">
  <h1 align="center">QuantaQ</h1>
  <p align="center">A lightweight, Redis-backed distributed job queue for Go.</p>
</p>

---

## Overview

QuantaQ is a simple yet powerful job queue built on top of Redis. It provides reliable job enqueuing, concurrent worker processing, automatic retries, and dead letter queue (DLQ) support — all with minimal setup.

### Key Features

- **Redis-backed persistence** — Jobs survive restarts and are shared across processes
- **Concurrent worker pool** — Process jobs in parallel with configurable concurrency
- **Automatic retries** — Failed jobs are re-enqueued up to a configurable max attempts
- **Dead letter queue** — Jobs exceeding max attempts are moved to DLQ for inspection
- **Batch enqueuing** — Enqueue multiple jobs in a single atomic Redis transaction
- **Pluggable clock** — Inject a custom clock for deterministic testing
- **Pluggable metrics** — Track enqueued, fetched, acked, nacked, and DLQ counts per queue
- **Atomic operations** — All state transitions use Redis transactions for consistency

## Installation

```bash
go get github.com/zukrein/quantaq
```

**Requirements:** Go 1.25+ and a running Redis instance.

## Quick Start

### Enqueue a Job

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    quantaqRedis "github.com/zukrein/quantaq/internal/redis"
    "github.com/zukrein/quantaq"
)

func main() {
    redisClient, err := quantaqRedis.NewClient("localhost:6379", "", 0)
    if err != nil {
        log.Fatal(err)
    }

    client := quantaq.NewClient(redisClient)
    ctx := context.Background()

    job, err := client.Enqueue(ctx, "email", []byte(`{"to":"user@example.com","subject":"Welcome!"}`), quantaq.EnqueueOptions{
        MaxAttempts: 5,
        RunAt:       time.Now().Add(1 * time.Minute),
        Metadata: map[string]string{
            "content_type": "application/json",
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Job enqueued: %s\n", job.ID)
}
```

### Process Jobs with a Worker

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os/signal"
    "syscall"
    "time"

    quantaqRedis "github.com/zukrein/quantaq/internal/redis"
    "github.com/zukrein/quantaq"
)

func main() {
    redisClient, err := quantaqRedis.NewClient("localhost:6379", "", 0)
    if err != nil {
        log.Fatal(err)
    }

    client := quantaq.NewClient(redisClient)

    worker := quantaq.NewWorker(client, quantaq.WorkerOptions{
        Concurrency:     10,
        PollInterval:    500 * time.Millisecond,
        ShutdownTimeout: 30 * time.Second,
    })

    worker.RegisterHandler("email", func(ctx context.Context, job *quantaq.Job) error {
        fmt.Printf("Processing job %s: %s\n", job.ID, string(job.Payload))
        // Your processing logic here
        return nil
    })

    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    fmt.Println("Worker started. Press Ctrl+C to stop.")
    worker.Start(ctx)
}
```

### Batch Enqueue

```go
jobs := []quantaq.Job{
    {Payload: []byte(`{"to":"alice@example.com"}`)},
    {Payload: []byte(`{"to":"bob@example.com"}`)},
    {Payload: []byte(`{"to":"charlie@example.com"}`)},
}

result, err := client.EnqueueBatch(ctx, "email", jobs, quantaq.EnqueueOptions{
    MaxAttempts: 3,
})
// result contains all 3 jobs with generated IDs
```

## API Reference

### Client

```go
// Create a new client with optional configuration
client := quantaq.NewClient(redisClient, opts ...quantaq.ClientOption)
```

| Method | Description |
|--------|-------------|
| `Enqueue(ctx, queue, payload, opts)` | Enqueue a single job |
| `EnqueueBatch(ctx, queue, jobs, opts)` | Enqueue multiple jobs atomically |
| `Fetch(ctx, queue)` | Fetch and lease the next available job |
| `Ack(ctx, queue, jobID)` | Acknowledge a successfully processed job |
| `Nack(ctx, queue, jobID, errMsg)` | Reject a job (re-enqueue or move to DLQ) |
| `Cancel(ctx, jobID)` | Cancel a pending or leased job |
| `GetJob(ctx, jobID)` | Retrieve job details by ID |
| `QueueStats(ctx, queue)` | Get counts: ready, leased, failed |
| `PurgeQueue(ctx, queue)` | Delete all jobs in a queue |

### Client Options

```go
// Inject a custom clock (useful for testing)
quantaq.WithClock(myClock)

// Inject a metrics collector
quantaq.WithMetrics(myCollector)
```

### Worker

```go
worker := quantaq.NewWorker(client, quantaq.WorkerOptions{
    Concurrency:     5,              // goroutines per queue (default: 5)
    PollInterval:    time.Second,    // polling frequency (default: 1s)
    ShutdownTimeout: 30 * time.Second, // graceful shutdown timeout (default: 30s)
})

worker.RegisterHandler("queue_name", handlerFunc)
worker.Start(ctx)
```

### EnqueueOptions

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `MaxAttempts` | `int` | `3` | Maximum number of processing attempts |
| `RunAt` | `time.Time` | zero | Scheduled execution time |
| `Metadata` | `map[string]string` | `nil` | Arbitrary key-value metadata |

## Job Lifecycle

```
  Enqueue
    │
    ▼
┌─────────┐   Fetch   ┌─────────┐   Ack   ┌─────────┐
│  ready   │ ────────► │ leased  │ ──────► │  acked  │
└─────────┘           └─────────┘         └─────────┘
                           │
                      Nack │
                           ▼
                   ┌──────────────┐
                   │ attempts < max?│
                   └──────────────┘
                    yes │      │ no
                        ▼      ▼
                   ┌────────┐ ┌─────┐
                   │ ready  │ │ dlq │
                   └────────┘ └─────┘
```

| Status | Description |
|--------|-------------|
| `ready` | Waiting in queue to be processed |
| `leased` | Currently being processed by a worker |
| `acked` | Successfully processed |
| `failed` | Marked as failed |
| `canceled` | Canceled before completion |
| `dlq` | Moved to dead letter queue after exceeding max attempts |

## Redis Schema

### Keys

| Key Pattern | Type | Description |
|-------------|------|-------------|
| `queue:{name}:waiting` | LIST | Jobs waiting to be processed |
| `queue:{name}:processing` | LIST | Jobs currently being processed |
| `queue:{name}:failed` | LIST | Jobs in the dead letter queue |
| `job:{id}` | HASH | Job data and status |

### Job Hash Fields

| Field | Description |
|-------|-------------|
| `data` | JSON-serialized job object |
| `status` | Current job status string |

## Metrics

QuantaQ ships with a pluggable metrics system. Use the built-in `InMemoryCollector` or implement the `Collector` interface for custom integrations (Prometheus, StatsD, etc.).

```go
collector := quantaq.NewInMemoryCollector()
client := quantaq.NewClient(redisClient, quantaq.WithMetrics(collector))

// After processing some jobs...
snapshot := collector.Snapshot()
for queue, m := range snapshot {
    fmt.Printf("Queue %s: enqueued=%d fetched=%d acked=%d nacked=%d dlq=%d\n",
        queue, m.Enqueued, m.Fetched, m.Acked, m.Nacked, m.DLQ)
}
```

### Collector Interface

```go
type Collector interface {
    JobEnqueued(queue string)
    JobFetched(queue string)
    JobAcked(queue string)
    JobNacked(queue string)
    JobDLQ(queue string)
    JobCanceled(queue string)
    Snapshot() map[string]*QueueMetrics
}
```

## Testing

QuantaQ uses [miniredis](https://github.com/alicebob/miniredis) for in-memory Redis testing — no external Redis instance required.

```bash
go test ./... -v
```

Inject a mock clock for deterministic time-based tests:

```go
type mockClock struct {
    now time.Time
}

func (m *mockClock) Now() time.Time { return m.now }

mc := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
client := quantaq.NewClient(redisClient, quantaq.WithClock(mc))
```

## Project Structure

```
quantaq/
├── client.go            # Client: Enqueue, Cancel, GetJob, QueueStats, PurgeQueue
├── clock.go             # Clock interface and RealClock implementation
├── job.go               # Job model and status constants
├── metrics.go           # Collector interface, InMemoryCollector, NoopCollector
├── processor.go         # Worker pool: NewWorker, RegisterHandler, Start
├── worker.go            # Fetch, Ack, Nack operations
├── internal/
│   └── redis/           # Redis client wrapper and key helpers
├── test/                # Unit tests (miniredis-based)
├── go.mod
└── README.md
```

## License

MIT