package quantaq

import (
	"context"
	"fmt"
	model "quantaq/internal/model"
	"sync"
	"time"
)

type HandlerFunc func(ctx context.Context, job *model.Job) error

type Worker struct {
	client          *Client
	handlers        map[string]HandlerFunc
	concurrency     int
	pollInterval    time.Duration
	shutdownTimeout time.Duration
	wg              sync.WaitGroup
}

type WorkerOptions struct {
	Concurrency     int
	PollInterval    time.Duration
	ShutdownTimeout time.Duration
}

func NewWorker(client *Client, options WorkerOptions) *Worker {
	if options.Concurrency <= 0 {
		options.Concurrency = 5
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	if options.ShutdownTimeout <= 0 {
		options.ShutdownTimeout = 30 * time.Second
	}
	return &Worker{
		client:          client,
		handlers:        make(map[string]HandlerFunc),
		concurrency:     options.Concurrency,
		pollInterval:    options.PollInterval,
		shutdownTimeout: options.ShutdownTimeout,
	}
}

func (w *Worker) RegisterHandler(queue string, handler HandlerFunc) {
	w.handlers[queue] = handler
}

func (w *Worker) Start(ctx context.Context) {
	for queue, handler := range w.handlers {
		for i := 0; i < w.concurrency; i++ {
			w.wg.Add(1)
			go w.processQueue(ctx, queue, handler)
		}
	}
	w.wg.Wait()
}

func (w *Worker) processQueue(ctx context.Context, queue string, handler HandlerFunc) {
	defer w.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return

		default:
			job, err := w.client.Fetch(ctx, queue)
			if err != nil {
				fmt.Printf("fetch job from queue %s: %v\n", queue, err)
				continue
			}

			if job == nil {
				time.Sleep(w.pollInterval)
				continue
			}

			if err := handler(ctx, job); err != nil {
				fmt.Printf("handle job %s from queue %s: %v\n", job.ID, queue, err)
				if err := w.client.Nack(ctx, queue, job.ID, err.Error()); err != nil {
					fmt.Printf("nack job %s from queue %s: %v\n", job.ID, queue, err)
				}
			} else {
				if err := w.client.Ack(ctx, queue, job.ID); err != nil {
					fmt.Printf("ack job %s from queue %s: %v\n", job.ID, queue, err)
				}
			}
			time.Sleep(w.pollInterval)
		}
	}
}
