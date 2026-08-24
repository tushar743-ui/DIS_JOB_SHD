package lock

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	srv := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { client.Close() })
	return client, srv
}

func TestAcquireGrantsExclusiveOwnership(t *testing.T) {
	rdb, _ := newRedis(t)
	ctx := context.Background()

	first, err := Acquire(ctx, rdb, "job", time.Minute)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	if _, err := Acquire(ctx, rdb, "job", time.Minute); !errors.Is(err, ErrNotAcquired) {
		t.Fatalf("second acquire error = %v, want ErrNotAcquired", err)
	}

	if err := first.Release(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}

	if _, err := Acquire(ctx, rdb, "job", time.Minute); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
}

func TestReleaseOnlyRemovesOwnToken(t *testing.T) {
	rdb, srv := newRedis(t)
	ctx := context.Background()

	stale, err := Acquire(ctx, rdb, "job", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	srv.FastForward(100 * time.Millisecond)

	successor, err := Acquire(ctx, rdb, "job", time.Minute)
	if err != nil {
		t.Fatalf("successor acquire after expiry: %v", err)
	}

	if err := stale.Release(ctx); err != nil {
		t.Fatalf("stale release: %v", err)
	}

	value, err := rdb.Get(ctx, "job").Result()
	if err != nil {
		t.Fatalf("lock key was deleted by the stale holder: %v", err)
	}
	if value != successor.Token() {
		t.Fatalf("lock key holds %q, want the successor token %q", value, successor.Token())
	}
}

func TestRefreshExtendsOnlyForTheOwner(t *testing.T) {
	rdb, srv := newRedis(t)
	ctx := context.Background()

	held, err := Acquire(ctx, rdb, "job", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	srv.FastForward(150 * time.Millisecond)
	if err := held.Refresh(ctx); err != nil {
		t.Fatalf("refresh by owner: %v", err)
	}

	srv.FastForward(150 * time.Millisecond)
	if _, err := Acquire(ctx, rdb, "job", time.Minute); !errors.Is(err, ErrNotAcquired) {
		t.Fatal("refresh did not extend the lease, another holder acquired it")
	}

	srv.FastForward(300 * time.Millisecond)
	if err := held.Refresh(ctx); !errors.Is(err, ErrLost) {
		t.Fatalf("refresh after expiry error = %v, want ErrLost", err)
	}
}

func TestFencingTokensIncreaseMonotonically(t *testing.T) {
	rdb, _ := newRedis(t)
	ctx := context.Background()

	var previous int64
	for i := range 5 {
		held, err := Acquire(ctx, rdb, "job", time.Minute)
		if err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
		if held.Fence() <= previous {
			t.Fatalf("fence token %d did not increase past %d", held.Fence(), previous)
		}
		previous = held.Fence()
		held.Release(ctx)
	}
}

func TestAcquireIsMutuallyExclusiveUnderContention(t *testing.T) {
	rdb, _ := newRedis(t)
	ctx := context.Background()

	const contenders = 50
	var granted atomic.Int64
	var wg sync.WaitGroup

	for range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Acquire(ctx, rdb, "hot", time.Minute); err == nil {
				granted.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := granted.Load(); got != 1 {
		t.Fatalf("%d contenders acquired the lock concurrently, want exactly 1", got)
	}
}

func TestGuardRunsWorkAndReleasesOnReturn(t *testing.T) {
	rdb, _ := newRedis(t)
	ctx, cancel := context.WithCancel(context.Background())

	ran := make(chan int64, 1)
	go Guard(ctx, rdb, "leader", GuardOptions{TTL: time.Second, RetryInterval: 20 * time.Millisecond},
		func(held context.Context, l *Lock) {
			ran <- l.Fence()
			<-held.Done()
		})

	select {
	case fence := <-ran:
		if fence < 1 {
			t.Fatalf("guard ran with fence %d, want a positive token", fence)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("guard never invoked the guarded function")
	}

	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := rdb.Get(context.Background(), "leader").Err(); errors.Is(err, redis.Nil) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("guard did not release the lock after its context was cancelled")
}

func TestGuardElectsExactlyOneLeader(t *testing.T) {
	rdb, _ := newRedis(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var concurrent, peak atomic.Int64
	var wg sync.WaitGroup

	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Guard(ctx, rdb, "leader", GuardOptions{TTL: time.Second, RetryInterval: 20 * time.Millisecond},
				func(held context.Context, _ *Lock) {
					now := concurrent.Add(1)
					for {
						old := peak.Load()
						if now <= old || peak.CompareAndSwap(old, now) {
							break
						}
					}
					<-held.Done()
					concurrent.Add(-1)
				})
		}()
	}

	time.Sleep(400 * time.Millisecond)
	cancel()
	wg.Wait()

	if got := peak.Load(); got != 1 {
		t.Fatalf("%d guards held leadership at once, want exactly 1", got)
	}
}

func TestGuardReportsUnavailableBackend(t *testing.T) {
	rdb, srv := newRedis(t)
	srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	degraded := make(chan error, 1)
	Guard(ctx, rdb, "leader", GuardOptions{
		TTL:           time.Second,
		RetryInterval: 20 * time.Millisecond,
		OnUnavailable: func(err error) {
			select {
			case degraded <- err:
			default:
			}
		},
	}, func(context.Context, *Lock) {
		t.Error("guarded work ran even though the lock backend was unreachable")
	})

	select {
	case err := <-degraded:
		if err == nil {
			t.Fatal("OnUnavailable called with a nil error")
		}
	default:
		t.Fatal("OnUnavailable was never called for an unreachable backend")
	}
}
