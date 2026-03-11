package quantaq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	quantaqRedis "github.com/zukreindev/quantaq/internal/redis"
)

func (c *Client) Fetch(ctx context.Context, queue string) (*Job, error) {
	if queue == "" {
		return nil, errors.New("queue name is required")
	}

	jobID, err := c.redis.LMove(ctx, quantaqRedis.WaitingKey(queue), quantaqRedis.ProcessingKey(queue), "RIGHT", "LEFT").Result()
	if err != nil {
		if errors.Is(err, quantaqRedis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("fetch job ID: %w", err)
	}

	jobKey := quantaqRedis.JobKey(jobID)

	data, err := c.redis.HGet(ctx, jobKey, "data").Bytes()
	if err != nil {
		return nil, fmt.Errorf("get job data: %w", err)
	}

	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("unmarshal job data: %w", err)
	}

	job.Status = StatusLeased
	job.Attempts++
	now := c.clock.Now()
	job.StartedAt = &now
	job.LastAttemptAt = &now

	updatedData, err := json.Marshal(job)
	if err != nil {
		return nil, fmt.Errorf("marshal updated job data: %w", err)
	}

	transaction := c.redis.TxPipeline()
	transaction.HSet(ctx, jobKey, "status", string(StatusLeased))
	transaction.HSet(ctx, jobKey, "data", updatedData)

	if _, err := transaction.Exec(ctx); err != nil {
		return nil, fmt.Errorf("update job status transaction: %w", err)
	}

	c.metrics.JobFetched(queue)
	return &job, nil
}

func (c *Client) Ack(ctx context.Context, queue, jobID string) error {
	if queue == "" {
		return errors.New("queue name is required")
	}

	if jobID == "" {
		return errors.New("job ID is required")
	}

	jobKey := quantaqRedis.JobKey(jobID)

	status, err := c.redis.HGet(ctx, jobKey, "status").Result()
	if err != nil {
		return fmt.Errorf("get job status: %w", err)
	}

	if status != string(StatusLeased) {
		return fmt.Errorf("cannot ack job with status %s", status)
	}

	rawData := c.redis.HGet(ctx, jobKey, "data").Val()
	if rawData == "" {
		return fmt.Errorf("job data not found for job ID %s", jobID)
	}

	var job Job
	if err := json.Unmarshal([]byte(rawData), &job); err != nil {
		return fmt.Errorf("unmarshal job data: %w", err)
	}

	now := c.clock.Now()
	job.Status = StatusAcked
	job.FinishedAt = &now

	jsonData, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job data: %w", err)
	}

	transaction := c.redis.TxPipeline()

	transaction.HSet(ctx, jobKey, "status", string(StatusAcked))
	transaction.HSet(ctx, jobKey, "data", jsonData)
	transaction.LRem(ctx, quantaqRedis.ProcessingKey(queue), 1, jobID)

	if _, err := transaction.Exec(ctx); err != nil {
		return fmt.Errorf("ack job transaction: %w", err)
	}

	c.metrics.JobAcked(queue)
	return nil
}

func (c *Client) Nack(ctx context.Context, queue, jobID, errorMessage string) error {
	if queue == "" {
		return errors.New("queue name is required")
	}

	if jobID == "" {
		return errors.New("job ID is required")
	}

	jobKey := quantaqRedis.JobKey(jobID)

	status, err := c.redis.HGet(ctx, jobKey, "status").Result()
	if err != nil {
		return fmt.Errorf("get job status: %w", err)
	}

	if status != string(StatusLeased) {
		return fmt.Errorf("cannot nack job with status %s", status)
	}

	rawData := c.redis.HGet(ctx, jobKey, "data").Val()
	if rawData == "" {
		return fmt.Errorf("job data not found for job ID %s", jobID)
	}

	var job Job
	if err := json.Unmarshal([]byte(rawData), &job); err != nil {
		return fmt.Errorf("unmarshal job data: %w", err)
	}

	now := c.clock.Now()
	job.Status = StatusReady
	job.LastAttemptAt = &now
	job.LastError = errorMessage
	job.Attempts++

	if job.Attempts >= job.MaxAttempts {
		job.Status = StatusDLQ
		job.FinishedAt = &now
	}

	jsonData, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job data: %w", err)
	}

	transaction := c.redis.TxPipeline()

	transaction.HSet(ctx, jobKey, "status", string(job.Status))
	transaction.HSet(ctx, jobKey, "data", jsonData)
	transaction.LRem(ctx, quantaqRedis.ProcessingKey(queue), 1, jobID)

	switch job.Status {
	case StatusReady:
		transaction.RPush(ctx, quantaqRedis.WaitingKey(queue), jobID)
	case StatusDLQ:
		transaction.RPush(ctx, quantaqRedis.FailedKey(queue), jobID)
	}

	if _, err := transaction.Exec(ctx); err != nil {
		return fmt.Errorf("nack job transaction: %w", err)
	}

	if job.Status == StatusDLQ {
		c.metrics.JobDLQ(queue)
	} else {
		c.metrics.JobNacked(queue)
	}
	return nil
}
