package quantaq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	model "quantaq/internal/model"
	quantaqRedis "quantaq/internal/storage/redis"

	"github.com/google/uuid"
)

type Client struct {
	redis *quantaqRedis.Client
}

func NewClient(redisClient *quantaqRedis.Client) *Client {
	return &Client{
		redis: redisClient,
	}
}

type EnqueueOptions struct {
	MaxAttempts int
	RunAt       time.Time
	Metadata    map[string]string
}

func (c *Client) Enqueue(ctx context.Context, queue string, payload []byte, options EnqueueOptions) (*model.Job, error) {
	if queue == "" {
		return nil, errors.New("queue name is required")
	}

	maxAttempts := options.MaxAttempts
	runAt := options.RunAt
	metadata := options.Metadata

	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	if metadata == nil {
		metadata = make(map[string]string)
	}

	now := time.Now().UTC()

	job := &model.Job{
		ID:          uuid.NewString(),
		Queue:       queue,
		Payload:     payload,
		Status:      model.StatusReady,
		MaxAttempts: maxAttempts,
		RunAt:       runAt.UTC(),
		CreatedAt:   now,
		Meta:        metadata,
	}

	jobKey := quantaqRedis.JobKey(job.ID)
	waitingKey := quantaqRedis.WaitingKey(queue)

	data, err := json.Marshal(job)
	if err != nil {
		return nil, fmt.Errorf("marshal job: %w", err)
	}

	pipe := c.redis.TxPipeline()

	pipe.HSet(ctx, jobKey, "data", data)
	pipe.HSet(ctx, jobKey, "status", string(model.StatusReady))
	pipe.LPush(ctx, waitingKey, job.ID)

	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("enqueue job: %w", err)
	}

	return job, nil
}

func (c *Client) EnqueueBatch(ctx context.Context, queue string, jobs []model.Job, options EnqueueOptions) ([]*model.Job, error) {
	if queue == "" {
		return nil, errors.New("queue name is required")
	}

	if len(jobs) == 0 {
		return nil, errors.New("jobs list is empty")
	}

	maxAttempts := options.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	now := time.Now().UTC()
	waitingKey := quantaqRedis.WaitingKey(queue)
	pipe := c.redis.TxPipeline()

	result := make([]*model.Job, 0, len(jobs))

	for i := range jobs {
		job := &jobs[i]
		job.ID = uuid.NewString()
		job.Queue = queue
		job.Status = model.StatusReady
		job.CreatedAt = now

		if job.MaxAttempts <= 0 {
			job.MaxAttempts = maxAttempts
		}
		if !options.RunAt.IsZero() {
			job.RunAt = options.RunAt.UTC()
		}
		if job.Meta == nil {
			job.Meta = options.Metadata
		}

		data, err := json.Marshal(job)
		if err != nil {
			return nil, fmt.Errorf("marshal job %d: %w", i, err)
		}

		jobKey := quantaqRedis.JobKey(job.ID)
		pipe.HSet(ctx, jobKey, "data", data)
		pipe.HSet(ctx, jobKey, "status", string(model.StatusReady))
		pipe.LPush(ctx, waitingKey, job.ID)

		result = append(result, job)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("enqueue batch: %w", err)
	}

	return result, nil
}

func (c *Client) Cancel(ctx context.Context, jobID string) error {
	if jobID == "" {
		return errors.New("job ID is required")
	}

	jobKey := quantaqRedis.JobKey(jobID)

	exists, err := c.redis.HExists(ctx, jobKey, "data").Result()
	if err != nil {
		return fmt.Errorf("check job exists: %w", err)
	}

	if !exists {
		return errors.New("cancel job: job not found")
	}

	currentStatus, err := c.redis.HGet(ctx, jobKey, "status").Result()
	if err != nil {
		return fmt.Errorf("get job status: %w", err)
	}

	if currentStatus == string(model.StatusAcked) || currentStatus == string(model.StatusFailed) || currentStatus == string(model.StatusCanceled) {
		return errors.New("cancel job: job already completed or canceled")
	}

	if _, err := c.redis.HSet(ctx, jobKey, "status", string(model.StatusCanceled)).Result(); err != nil {
		return fmt.Errorf("cancel job: %w", err)
	}

	return nil
}


func (c *Client) GetJob(ctx context.Context, jobID string) (*model.Job, error) {
	if jobID == "" {
		return nil, errors.New("job ID is required")
	}

	jobKey := quantaqRedis.JobKey(jobID)

	data, err := c.redis.HGet(ctx, jobKey, "data").Bytes()
	if err != nil {
		return nil, fmt.Errorf("get job data: %w", err)
	}

	var job model.Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("unmarshal job data: %w", err)
	}

	return &job, nil
}


func (c *Client) QueueStats(ctx context.Context, queue string) (ready, leased, failed int64, err error) {
	if queue == "" {
		err = errors.New("queue name is required")
		return
	}

	waitingKey := quantaqRedis.WaitingKey(queue)
	processingKey := quantaqRedis.ProcessingKey(queue)
	failedKey := quantaqRedis.FailedKey(queue)
	
	ready, err = c.redis.LLen(ctx, waitingKey).Result()
	if err != nil {
		err = fmt.Errorf("get ready count: %w", err)
		return
	}

	leased, err = c.redis.LLen(ctx, processingKey).Result()
	if err != nil {
		err = fmt.Errorf("get leased count: %w", err)
		return
	}

	failed, err = c.redis.LLen(ctx, failedKey).Result()
	if err != nil {
		err = fmt.Errorf("get failed count: %w", err)
		return
	}

	return ready, leased, failed, nil
}

func (c *Client) PurgeQueue(ctx context.Context, queue string) error {
	if queue == "" {
		return errors.New("queue name is required")
	}
	
	waitingKey := quantaqRedis.WaitingKey(queue)
	processingKey := quantaqRedis.ProcessingKey(queue)
	failedKey := quantaqRedis.FailedKey(queue)

	pipe := c.redis.TxPipeline()
	pipe.Del(ctx, waitingKey)
	pipe.Del(ctx, processingKey)
	pipe.Del(ctx, failedKey)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("purge queue: %w", err)
	}

	return nil
}