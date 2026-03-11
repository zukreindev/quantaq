package model

import (
	"time"
)

type JobStatus string

const (
	StatusReady    JobStatus = "ready"
	StatusLeased   JobStatus = "leased"
	StatusAcked    JobStatus = "acked"
	StatusFailed   JobStatus = "failed"
	StatusCanceled JobStatus = "canceled"
	StatusDLQ      JobStatus = "dlq"
)

type Job struct {
	ID            string            `json:"id"`
	Queue         string            `json:"queue"`
	Payload       []byte            `json:"payload"`
	Meta          map[string]string `json:"meta,omitempty"`
	Status        JobStatus         `json:"status"`
	Attempts      int               `json:"attempts"`
	MaxAttempts   int               `json:"max_attempts"`
	RunAt         time.Time         `json:"run_at"`
	CreatedAt     time.Time         `json:"created_at"`
	StartedAt     *time.Time        `json:"started_at,omitempty"`
	FinishedAt    *time.Time        `json:"finished_at,omitempty"`
	LastAttemptAt *time.Time        `json:"last_attempt_at,omitempty"`
	LastError     string            `json:"last_error,omitempty"`
}
