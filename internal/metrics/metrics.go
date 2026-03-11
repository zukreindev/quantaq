package metrics

import "go.uber.org/atomic"

type Collector interface {
	JobEnqueued(queue string)
	JobFetched(queue string)
	JobAcked(queue string)
	JobNacked(queue string)
	JobDLQ(queue string)
	JobCanceled(queue string)
	Snapshot() map[string]*QueueMetrics
}

type QueueMetrics struct {
	Enqueued int64 `json:"enqueued"`
	Fetched  int64 `json:"fetched"`
	Acked    int64 `json:"acked"`
	Nacked   int64 `json:"nacked"`
	DLQ      int64 `json:"dlq"`
	Canceled int64 `json:"canceled"`
}

type InMemoryCollector struct {
	queues map[string]*queueCounters
}

type queueCounters struct {
	enqueued atomic.Int64
	fetched  atomic.Int64
	acked    atomic.Int64
	nacked   atomic.Int64
	dlq      atomic.Int64
	canceled atomic.Int64
}

func NewInMemoryCollector() *InMemoryCollector {
	return &InMemoryCollector{
		queues: make(map[string]*queueCounters),
	}
}

func (c *InMemoryCollector) get(queue string) *queueCounters {
	if q, ok := c.queues[queue]; ok {
		return q
	}
	q := &queueCounters{}
	c.queues[queue] = q
	return q
}

func (c *InMemoryCollector) JobEnqueued(queue string) { c.get(queue).enqueued.Inc() }
func (c *InMemoryCollector) JobFetched(queue string)  { c.get(queue).fetched.Inc() }
func (c *InMemoryCollector) JobAcked(queue string)    { c.get(queue).acked.Inc() }
func (c *InMemoryCollector) JobNacked(queue string)   { c.get(queue).nacked.Inc() }
func (c *InMemoryCollector) JobDLQ(queue string)      { c.get(queue).dlq.Inc() }
func (c *InMemoryCollector) JobCanceled(queue string) { c.get(queue).canceled.Inc() }

func (c *InMemoryCollector) Snapshot() map[string]*QueueMetrics {
	result := make(map[string]*QueueMetrics, len(c.queues))
	for name, q := range c.queues {
		result[name] = &QueueMetrics{
			Enqueued: q.enqueued.Load(),
			Fetched:  q.fetched.Load(),
			Acked:    q.acked.Load(),
			Nacked:   q.nacked.Load(),
			DLQ:      q.dlq.Load(),
			Canceled: q.canceled.Load(),
		}
	}
	return result
}

type NoopCollector struct{}

func (NoopCollector) JobEnqueued(string)                 {}
func (NoopCollector) JobFetched(string)                  {}
func (NoopCollector) JobAcked(string)                    {}
func (NoopCollector) JobNacked(string)                   {}
func (NoopCollector) JobDLQ(string)                      {}
func (NoopCollector) JobCanceled(string)                 {}
func (NoopCollector) Snapshot() map[string]*QueueMetrics { return nil }
