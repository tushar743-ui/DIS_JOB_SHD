package events

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newRedis(t *testing.T) *redis.Client {
	t.Helper()
	srv := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { client.Close() })
	return client
}

func TestWakesOnlyForClaimableWork(t *testing.T) {
	wakes := map[Type]bool{
		JobEnqueued:     true,
		JobUnblocked:    true,
		QueueResumed:    true,
		JobStarted:      false,
		JobCompleted:    false,
		JobFailed:       false,
		JobDeadLettered: false,
		JobCancelled:    false,
		QueuePaused:     false,
		WorkerOnline:    false,
		WorkerOffline:   false,
		WorkerHeartbeat: false,
	}

	for kind, want := range wakes {
		if got := (Event{Type: kind}).Wakes(); got != want {
			t.Errorf("%s.Wakes() = %v, want %v", kind, got, want)
		}
	}
}

func TestChannelIsScopedPerProject(t *testing.T) {
	if a, b := Channel("proj-a"), Channel("proj-b"); a == b {
		t.Fatal("two projects share a channel, events would leak across tenants")
	}
	if got := Channel("proj-a"); got != "djq:events:proj-a" {
		t.Fatalf("Channel = %q, want djq:events:proj-a", got)
	}
}

func TestEventSurvivesRoundTrip(t *testing.T) {
	original := Event{
		Type: JobFailed, ProjectID: "p1", QueueID: "q1", JobID: "j1",
		WorkerID: "w1", JobType: "send_email", Status: "queued",
		Shard: 7, Attempt: 2, Error: "smtp timeout",
		At: time.Now().UTC().Truncate(time.Second),
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Event
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded != original {
		t.Fatalf("round trip changed the event:\n got %+v\nwant %+v", decoded, original)
	}
}

func TestPublishDeliversToSubscriber(t *testing.T) {
	rdb := newRedis(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := Subscribe(ctx, rdb, "proj-a")
	waitForSubscriber(t, rdb, "djq:events:proj-a")

	bus := NewPublisher(rdb, nil)
	bus.Publish(ctx, Event{Type: JobEnqueued, ProjectID: "proj-a", JobID: "j1"})

	select {
	case got := <-stream:
		if got.Type != JobEnqueued || got.JobID != "j1" {
			t.Fatalf("received %+v, want a job.enqueued for j1", got)
		}
		if got.At.IsZero() {
			t.Fatal("publisher did not stamp the event time")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("subscriber never received the published event")
	}
}

func TestSubscriberIsIsolatedFromOtherProjects(t *testing.T) {
	rdb := newRedis(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := Subscribe(ctx, rdb, "proj-a")
	waitForSubscriber(t, rdb, "djq:events:proj-a")

	bus := NewPublisher(rdb, nil)
	bus.Publish(ctx, Event{Type: JobEnqueued, ProjectID: "proj-b", JobID: "other"})
	bus.Publish(ctx, Event{Type: JobEnqueued, ProjectID: "proj-a", JobID: "mine"})

	select {
	case got := <-stream:
		if got.JobID != "mine" {
			t.Fatalf("received %q, another project's event leaked through", got.JobID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("subscriber never received its own project's event")
	}
}

func TestPublishIgnoresEventsWithoutAProject(t *testing.T) {
	rdb := newRedis(t)
	failures := 0
	bus := NewPublisher(rdb, func(error) { failures++ })

	bus.Publish(context.Background(), Event{Type: JobEnqueued})

	if failures != 0 {
		t.Fatalf("publishing an unscoped event reported %d errors, want it dropped silently", failures)
	}
}

func TestPublishSurvivesADeadBroker(t *testing.T) {
	srv := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	defer rdb.Close()
	srv.Close()

	reported := make(chan error, 1)
	bus := NewPublisher(rdb, func(err error) {
		select {
		case reported <- err:
		default:
		}
	})

	done := make(chan struct{})
	go func() {
		bus.Publish(context.Background(), Event{Type: JobEnqueued, ProjectID: "p1"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked the caller when the broker was unreachable")
	}

	select {
	case err := <-reported:
		if err == nil {
			t.Fatal("error callback fired with a nil error")
		}
	default:
		t.Fatal("a failed publish was not reported to the error callback")
	}
}

func TestPublishHonoursACancelledCallerContext(t *testing.T) {
	rdb := newRedis(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	failures := 0
	bus := NewPublisher(rdb, func(error) { failures++ })
	bus.Publish(ctx, Event{Type: JobEnqueued, ProjectID: "p1"})

	if failures != 0 {
		t.Fatalf("publish failed with an already-cancelled caller context (%d errors); "+
			"lifecycle events must outlive the request that triggered them", failures)
	}
}

func waitForSubscriber(t *testing.T, rdb *redis.Client, channel string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		counts, err := rdb.PubSubNumSub(context.Background(), channel).Result()
		if err == nil && counts[channel] > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("subscription to " + channel + " never became active")
}
