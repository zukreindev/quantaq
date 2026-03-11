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
		ID: uuid.NewString(),
		Queue: queue,
		Payload: payload,
		Status: model.StatusReady,
		MaxAttempts: maxAttempts,
		RunAt: runAt.UTC(),
		CreatedAt: now,
		Meta: metadata,
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

func (c *Client) Fetch(ctx context.Context, queue string) (*model.Job, error) {
	if queue == "" {
		return nil, errors.New("queue name is required")
	}


	return nil, nil

}