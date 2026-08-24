package events

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

type Type string

const (
	JobEnqueued     Type = "job.enqueued"
	JobStarted      Type = "job.started"
	JobCompleted    Type = "job.completed"
	JobFailed       Type = "job.failed"
	JobDeadLettered Type = "job.dead_lettered"
	JobUnblocked    Type = "job.unblocked"
	JobCancelled    Type = "job.cancelled"
	QueuePaused     Type = "queue.paused"
	QueueResumed    Type = "queue.resumed"
	WorkerOnline    Type = "worker.online"
	WorkerOffline   Type = "worker.offline"
	WorkerHeartbeat Type = "worker.heartbeat"
)

type Event struct {
	Type      Type      `json:"type"`
	ProjectID string    `json:"project_id"`
	QueueID   string    `json:"queue_id,omitempty"`
	JobID     string    `json:"job_id,omitempty"`
	WorkerID  string    `json:"worker_id,omitempty"`
	JobType   string    `json:"job_type,omitempty"`
	Status    string    `json:"status,omitempty"`
	Shard     int       `json:"shard"`
	Attempt   int       `json:"attempt,omitempty"`
	Error     string    `json:"error,omitempty"`
	At        time.Time `json:"at"`
}

func (e Event) Wakes() bool {
	switch e.Type {
	case JobEnqueued, JobUnblocked, QueueResumed:
		return true
	}
	return false
}

func Channel(projectID string) string { return "djq:events:" + projectID }

const publishTimeout = 2 * time.Second

type Publisher struct {
	rdb *redis.Client
	err func(error)
}

func NewPublisher(rdb *redis.Client, onError func(error)) *Publisher {
	if onError == nil {
		onError = func(error) {}
	}
	return &Publisher{rdb: rdb, err: onError}
}

func (p *Publisher) Publish(ctx context.Context, e Event) {
	if p == nil || p.rdb == nil || e.ProjectID == "" {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	payload, err := json.Marshal(e)
	if err != nil {
		p.err(err)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()
	if err := p.rdb.Publish(ctx, Channel(e.ProjectID), payload).Err(); err != nil {
		p.err(err)
	}
}

func Subscribe(ctx context.Context, rdb *redis.Client, projectID string) <-chan Event {
	sub := rdb.Subscribe(ctx, Channel(projectID))
	out := make(chan Event, 128)

	go func() {
		defer close(out)
		defer sub.Close()
		ch := sub.Channel(redis.WithChannelSize(128))
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var e Event
				if err := json.Unmarshal([]byte(msg.Payload), &e); err != nil {
					continue
				}
				select {
				case out <- e:
				case <-ctx.Done():
					return
				default:
				}
			}
		}
	}()

	return out
}
