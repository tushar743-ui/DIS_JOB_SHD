package executor

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCalcDelay_NoPolicy_ExponentialBackoff(t *testing.T) {
	e := &Executor{}
	job := &Job{RetryPolicy: nil}

	cases := []struct {
		attempt int
		wantMax time.Duration
	}{
		{1, 2 * time.Second},
		{2, 3 * time.Second},
		{3, 5 * time.Second},
		{4, 9 * time.Second},
		{10, 5*time.Minute + 1},
	}
	for _, c := range cases {
		d := e.calcDelay(job, c.attempt)
		if d > c.wantMax {
			t.Errorf("attempt=%d: delay %v exceeded expected max %v", c.attempt, d, c.wantMax)
		}
		if d <= 0 {
			t.Errorf("attempt=%d: delay must be positive, got %v", c.attempt, d)
		}
	}
}

func TestCalcDelay_Fixed(t *testing.T) {
	e := &Executor{}
	job := &Job{
		RetryPolicy: &RetryPolicy{
			Strategy:       "fixed",
			InitialDelayMs: 2000,
			MaxDelayMs:     60000,
		},
	}
	for _, attempt := range []int{1, 2, 5, 10} {
		d := e.calcDelay(job, attempt)
		if d != 2*time.Second {
			t.Errorf("attempt=%d: fixed delay should be 2s, got %v", attempt, d)
		}
	}
}

func TestCalcDelay_Linear(t *testing.T) {
	e := &Executor{}
	job := &Job{
		RetryPolicy: &RetryPolicy{
			Strategy:       "linear",
			InitialDelayMs: 1000,
			MaxDelayMs:     5000,
		},
	}

	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 3 * time.Second},
		{5, 5 * time.Second},
		{6, 5 * time.Second},
	}
	for _, c := range cases {
		d := e.calcDelay(job, c.attempt)
		if d != c.want {
			t.Errorf("attempt=%d: expected %v, got %v", c.attempt, c.want, d)
		}
	}
}

func TestCalcDelay_Exponential(t *testing.T) {
	e := &Executor{}
	job := &Job{
		RetryPolicy: &RetryPolicy{
			Strategy:       "exponential",
			InitialDelayMs: 500,
			MaxDelayMs:     16000,
			Multiplier:     2.0,
		},
	}

	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 500 * time.Millisecond},
		{2, 1000 * time.Millisecond},
		{3, 2000 * time.Millisecond},
		{4, 4000 * time.Millisecond},
		{5, 8000 * time.Millisecond},
		{6, 16000 * time.Millisecond},
		{10, 16000 * time.Millisecond},
	}
	for _, c := range cases {
		d := e.calcDelay(job, c.attempt)
		if d != c.want {
			t.Errorf("attempt=%d: expected %v, got %v", c.attempt, c.want, d)
		}
	}
}

func TestCalcDelay_DefaultFallback_CapAt5min(t *testing.T) {
	e := &Executor{}
	job := &Job{RetryPolicy: nil}
	cap := 5 * time.Minute
	for attempt := 1; attempt <= 30; attempt++ {
		d := e.calcDelay(job, attempt)
		if d > cap {
			t.Errorf("attempt=%d: delay %v exceeded 5-minute cap", attempt, d)
		}
	}
}

func TestRegister_And_Lookup(t *testing.T) {
	e := &Executor{handlers: map[string]Handler{}}
	called := false
	e.Register("my_job", func(ctx context.Context, job *Job) error {
		called = true
		return nil
	})

	e.mu.Lock()
	h, ok := e.handlers["my_job"]
	e.mu.Unlock()

	if !ok {
		t.Fatal("handler not registered")
	}
	if err := h(context.Background(), &Job{}); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
}

func TestRegister_Overwrite(t *testing.T) {
	e := &Executor{handlers: map[string]Handler{}}
	e.Register("job", func(ctx context.Context, job *Job) error { return errors.New("v1") })
	e.Register("job", func(ctx context.Context, job *Job) error { return nil })

	e.mu.Lock()
	h := e.handlers["job"]
	e.mu.Unlock()

	if err := h(context.Background(), &Job{}); err != nil {
		t.Fatalf("overwritten handler should return nil, got: %v", err)
	}
}

func TestSemChan_RespectsCapacity(t *testing.T) {
	concurrency := 3
	e := &Executor{sem: make(chan struct{}, concurrency)}
	if cap(e.SemChan()) != concurrency {
		t.Fatalf("expected semaphore capacity=%d, got %d", concurrency, cap(e.SemChan()))
	}
}

func TestCalcDelay_UnknownStrategy_FallsBackToExponential(t *testing.T) {
	e := &Executor{}
	job := &Job{
		RetryPolicy: &RetryPolicy{
			Strategy:       "unknown-strategy",
			InitialDelayMs: 1000,
			MaxDelayMs:     10000,
			Multiplier:     2.0,
		},
	}
	d1 := e.calcDelay(job, 1)
	d2 := e.calcDelay(job, 2)
	if d1 <= 0 || d2 <= 0 {
		t.Fatal("delay must be positive for unknown strategy")
	}
	if d2 <= d1 {
		t.Fatalf("delay should grow with attempts: d1=%v d2=%v", d1, d2)
	}
}

func TestDrain_ReturnsWhenDone(t *testing.T) {
	e := &Executor{sem: make(chan struct{}, 2)}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	e.Drain(ctx)
	if ctx.Err() != nil {
		t.Fatal("Drain should have returned before timeout with no running jobs")
	}
}

func TestDrain_RespectsContextTimeout(t *testing.T) {
	e := &Executor{sem: make(chan struct{}, 1)}
	e.wg.Add(1)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	e.Drain(ctx)
	if ctx.Err() == nil {
		t.Fatal("Drain should have timed out with a stuck job")
	}
	e.wg.Done()
}
