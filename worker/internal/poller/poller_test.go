//go:build integration

package poller_test

import (
	"context"
	"encoding/hex"
	"fmt"
	mrand "math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tushar/dis-job-queue/worker/internal/config"
	workerdb "github.com/tushar/dis-job-queue/worker/internal/db"
	"github.com/tushar/dis-job-queue/worker/internal/executor"
	"github.com/tushar/dis-job-queue/worker/internal/poller"
)

var (
	testPool      *pgxpool.Pool
	testProjectID string
	testQueueID   string
)

func TestMain(m *testing.M) {
	if os.Getenv("DATABASE_URL") == "" || os.Getenv("PROJECT_ID") == "" {
		fmt.Println("SKIP: DATABASE_URL or PROJECT_ID not set — integration tests require a real database")
		os.Exit(0)
	}

	cfg := config.Load()
	// Increase pool size for concurrent worker tests
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse db url: %v\n", err)
		os.Exit(1)
	}
	poolCfg.MaxConns = 40
	poolCfg.MinConns = 5
	ctx0 := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx0, poolCfg)
	if err != nil {
		// fall back to standard pool
		pool, err = workerdb.NewPool(cfg.DatabaseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "db connect: %v\n", err)
			os.Exit(1)
		}
	}
	testPool = pool
	testProjectID = cfg.ProjectID

	// Resolve a queue ID for this project
	ctx := context.Background()
	var qid string
	err = pool.QueryRow(ctx,
		`SELECT id FROM queues WHERE project_id=$1 LIMIT 1`, testProjectID,
	).Scan(&qid)
	if err != nil || qid == "" {
		fmt.Fprintln(os.Stderr, "no queues found for PROJECT_ID — run API integration tests first to create them")
		os.Exit(0)
	}
	testQueueID = qid

	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// insertJob inserts a job directly into DB and returns its ID.
func insertJob(t *testing.T, jobType string, priority int) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(),
		`INSERT INTO jobs (queue_id, type, payload, status, priority, max_attempts, run_at, timeout_secs)
		 VALUES ($1,$2,'{}','queued',$3,3,now(),30) RETURNING id`,
		testQueueID, jobType, priority,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertJob: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM jobs WHERE id=$1`, id)
	})
	return id
}

func jobStatus(t *testing.T, jobID string) string {
	t.Helper()
	var status string
	testPool.QueryRow(context.Background(),
		`SELECT status::text FROM jobs WHERE id=$1`, jobID,
	).Scan(&status)
	return status
}

func newWorkerID() string {
	return fmt.Sprintf("test-worker-%d", mrand.Int63())
}

func workerHex() string {
	b := make([]byte, 8)
	for i := range b {
		b[i] = byte(mrand.Intn(256))
	}
	return hex.EncodeToString(b)
}

// registerWorker creates a worker row and returns its ID.
func registerWorker(t *testing.T, projectID string) string {
	t.Helper()
	var wid string
	err := testPool.QueryRow(context.Background(),
		`INSERT INTO workers (project_id, hostname, pid, status, concurrency, queue_ids)
		 VALUES ($1,$2,$3,'active',5,'{}') RETURNING id`,
		projectID, "test-host-"+workerHex(), os.Getpid(),
	).Scan(&wid)
	if err != nil {
		t.Fatalf("registerWorker: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workers WHERE id=$1`, wid)
	})
	return wid
}

// ─── Priority ordering ────────────────────────────────────────────────────────

// TestPriorityOrdering verifies that jobs with higher priority numbers are
// claimed before lower-priority ones (ORDER BY priority DESC).
// Uses concurrency=1 and an isolated queue so jobs are claimed one at a time
// with no competing jobs from other tests.
func TestPriorityOrdering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Create an isolated queue for this test so no other jobs compete.
	queueName := "prio_test_" + workerHex()
	var isolatedQueueID string
	err := testPool.QueryRow(ctx,
		`INSERT INTO queues (project_id, name, paused) VALUES ($1,$2,false) RETURNING id`,
		testProjectID, queueName,
	).Scan(&isolatedQueueID)
	if err != nil {
		t.Fatalf("create isolated queue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM jobs WHERE queue_id=$1`, isolatedQueueID)
		testPool.Exec(context.Background(), `DELETE FROM queues WHERE id=$1`, isolatedQueueID)
	})

	// Insert 5 jobs with distinct priorities in reverse insertion order to prove SQL ordering.
	priorities := []int{1, 3, 5, 7, 9}
	for _, prio := range priorities {
		var id string
		testPool.QueryRow(ctx,
			`INSERT INTO jobs (queue_id, type, payload, status, priority, max_attempts, run_at, timeout_secs)
			 VALUES ($1,'prio_test_serial','{}','queued',$2,3,now(),30) RETURNING id`,
			isolatedQueueID, prio,
		).Scan(&id)
	}

	workerID := registerWorker(t, testProjectID)

	// concurrency=1 — poller claims one job per cycle, so SQL ORDER BY governs sequence.
	exec := executor.New(testPool, nil, workerID, &config.Config{Concurrency: 1})

	var mu sync.Mutex
	var processedPriorities []int
	done := make(chan struct{})

	exec.Register("prio_test_serial", func(ctx context.Context, job *executor.Job) error {
		var prio int
		testPool.QueryRow(context.Background(),
			`SELECT priority FROM jobs WHERE id=$1`, job.ID).Scan(&prio)
		mu.Lock()
		processedPriorities = append(processedPriorities, prio)
		n := len(processedPriorities)
		mu.Unlock()
		if n == len(priorities) {
			close(done)
		}
		return nil
	})

	cfg := &config.Config{
		ProjectID:    testProjectID,
		QueueNames:   []string{queueName},
		Concurrency:  1,
		PollInterval: 50 * time.Millisecond,
	}
	p := poller.New(testPool, nil, exec, workerID, cfg)
	p.ResolveQueueIDs(ctx)

	pollCtx, pollCancel := context.WithTimeout(ctx, 15*time.Second)
	defer pollCancel()
	go p.Run(pollCtx)

	select {
	case <-done:
	case <-time.After(12 * time.Second):
		mu.Lock()
		n := len(processedPriorities)
		mu.Unlock()
		t.Fatalf("only %d/5 jobs processed within timeout (queue=%s)", n, isolatedQueueID)
	}

	mu.Lock()
	order := make([]int, len(processedPriorities))
	copy(order, processedPriorities)
	mu.Unlock()

	t.Logf("processing order: %v (expected descending: [9 7 5 3 1])", order)

	for i := 1; i < len(order); i++ {
		if order[i] > order[i-1] {
			t.Errorf("priority ordering violated at position %d: priority=%d after priority=%d",
				i, order[i], order[i-1])
		}
	}
}

// ─── Skip-locked: no double-claim ────────────────────────────────────────────

// TestSkipLocked verifies that two concurrent workers each claim distinct jobs
// (FOR UPDATE SKIP LOCKED prevents double-claiming).
func TestSkipLocked_NoDuplicateClaims(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const numJobs = 20
	jobIDs := make([]string, numJobs)
	for i := range jobIDs {
		jobIDs[i] = insertJob(t, "skip_lock_test", 5)
	}

	var claimed sync.Map // jobID → workerID
	var duplicates int32

	workerCount := 5
	var wg sync.WaitGroup

	for w := 0; w < workerCount; w++ {
		wid := registerWorker(t, testProjectID)
		cfg := &config.Config{
			ProjectID:    testProjectID,
			QueueNames:   []string{"default"},
			Concurrency:  5,
			PollInterval: 50 * time.Millisecond,
		}
		exec := executor.New(testPool, nil, wid, &config.Config{Concurrency: 5})
		exec.Register("skip_lock_test", func(ctx context.Context, job *executor.Job) error {
			if _, loaded := claimed.LoadOrStore(job.ID, wid); loaded {
				atomic.AddInt32(&duplicates, 1)
				t.Errorf("job %s was claimed by two workers (SKIP LOCKED violation)", job.ID)
			}
			return nil
		})

		p := poller.New(testPool, nil, exec, wid, cfg)
		p.ResolveQueueIDs(ctx)

		wg.Add(1)
		go func() {
			defer wg.Done()
			pollCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			p.Run(pollCtx)
		}()
	}

	// Wait until all jobs are claimed or timeout
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		count := 0
		claimed.Range(func(_, _ any) bool { count++; return true })
		if count >= numJobs {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	wg.Wait()

	if dups := atomic.LoadInt32(&duplicates); dups > 0 {
		t.Errorf("detected %d double-claimed jobs — FOR UPDATE SKIP LOCKED may not be working", dups)
	}

	count := 0
	claimed.Range(func(_, _ any) bool { count++; return true })
	t.Logf("skip-locked test: %d/%d jobs claimed with no duplicates", count, numJobs)
}

// ─── Scheduler: promotes scheduled jobs ──────────────────────────────────────

func TestScheduler_PromotesScheduledJobs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Insert a job that is scheduled 1 second from now
	var jobID string
	runAt := time.Now().Add(1 * time.Second)
	err := testPool.QueryRow(ctx,
		`INSERT INTO jobs (queue_id, type, payload, status, priority, max_attempts, run_at, timeout_secs)
		 VALUES ($1,'sched_promote','{}','scheduled',5,3,$2,30) RETURNING id`,
		testQueueID, runAt,
	).Scan(&jobID)
	if err != nil {
		t.Fatalf("insert scheduled job: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM jobs WHERE id=$1`, jobID)
	})

	// Verify it starts as 'scheduled'
	if s := jobStatus(t, jobID); s != "scheduled" {
		t.Fatalf("expected status=scheduled before promotion, got %s", s)
	}

	// Run the scheduler
	cfg := &config.Config{
		ProjectID:    testProjectID,
		QueueNames:   []string{"default"},
		Concurrency:  1,
		PollInterval: 100 * time.Millisecond,
	}
	wid := registerWorker(t, testProjectID)
	exec := executor.New(testPool, nil, wid, &config.Config{Concurrency: 1})
	p := poller.New(testPool, nil, exec, wid, cfg)

	schedCtx, schedCancel := context.WithTimeout(ctx, 10*time.Second)
	defer schedCancel()
	go p.RunScheduler(schedCtx)

	// Wait up to 8s for promotion (scheduler runs every 5s)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if s := jobStatus(t, jobID); s == "queued" {
			t.Log("job promoted from scheduled → queued")
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("job was not promoted from scheduled to queued within 8s (status=%s)", jobStatus(t, jobID))
}

// ─── Failure path: job moves to DLQ after max_attempts ───────────────────────

func TestFailure_MovesToDLQ(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Insert a job with max_attempts=2 — our handler will always fail.
	var jobID string
	err := testPool.QueryRow(ctx,
		`INSERT INTO jobs (queue_id, type, payload, status, priority, max_attempts, run_at, timeout_secs)
		 VALUES ($1,'always_fail_dlq','{}','queued',5,2,now(),10) RETURNING id`,
		testQueueID,
	).Scan(&jobID)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM dead_letter_queue WHERE job_id=$1`, jobID)
		testPool.Exec(context.Background(), `DELETE FROM jobs WHERE id=$1`, jobID)
	})

	wid := registerWorker(t, testProjectID)
	cfg := &config.Config{
		ProjectID:    testProjectID,
		QueueNames:   []string{"default"},
		Concurrency:  2,
		PollInterval: 200 * time.Millisecond,
	}
	exec := executor.New(testPool, nil, wid, &config.Config{Concurrency: 2})
	exec.Register("always_fail_dlq", func(ctx context.Context, job *executor.Job) error {
		return fmt.Errorf("intentional failure for DLQ test")
	})

	p := poller.New(testPool, nil, exec, wid, cfg)
	p.ResolveQueueIDs(ctx)
	pollCtx, pollCancel := context.WithTimeout(ctx, 15*time.Second)
	defer pollCancel()
	go p.Run(pollCtx)

	// Wait for the job to reach 'dead' status
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if s := jobStatus(t, jobID); s == "dead" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	finalStatus := jobStatus(t, jobID)
	if finalStatus != "dead" {
		t.Fatalf("job should be 'dead' after exhausting max_attempts=2, got %s", finalStatus)
	}

	// Verify DLQ entry
	var dlqCount int
	testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM dead_letter_queue WHERE job_id=$1`, jobID,
	).Scan(&dlqCount)
	if dlqCount != 1 {
		t.Errorf("expected 1 DLQ entry, got %d", dlqCount)
	}
	t.Log("job correctly moved to DLQ after exhausting retries")
}

// ─── Paused queue is skipped by poller ───────────────────────────────────────

func TestPausedQueue_JobsNotClaimed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Pause the queue
	testPool.Exec(ctx, `UPDATE queues SET paused=true WHERE id=$1`, testQueueID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE queues SET paused=false WHERE id=$1`, testQueueID)
	})

	jobID := insertJob(t, "paused_queue_test", 5)

	wid := registerWorker(t, testProjectID)
	cfg := &config.Config{
		ProjectID:    testProjectID,
		QueueNames:   []string{"default"},
		Concurrency:  2,
		PollInterval: 200 * time.Millisecond,
	}
	exec := executor.New(testPool, nil, wid, &config.Config{Concurrency: 2})
	exec.Register("paused_queue_test", func(ctx context.Context, job *executor.Job) error {
		return nil
	})

	p := poller.New(testPool, nil, exec, wid, cfg)
	// ResolveQueueIDs excludes paused queues, so queueIDs will be empty
	p.ResolveQueueIDs(ctx)

	pollCtx, pollCancel := context.WithTimeout(ctx, 3*time.Second)
	defer pollCancel()
	go p.Run(pollCtx)

	// Wait 3s and verify job is still 'queued' (not claimed)
	time.Sleep(3 * time.Second)
	status := jobStatus(t, jobID)
	if status != "queued" {
		t.Errorf("paused queue: job should remain 'queued', got %s", status)
	}
	t.Log("paused queue correctly prevents job claiming")
}

// ─── Load test: 20 concurrent workers, 200 jobs ──────────────────────────────

func TestLoad_20Workers_200Jobs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	const numJobs = 200
	const numWorkers = 20

	// Workers share a parent context. Poll contexts are bounded to 50s so
	// goroutines exit cleanly before the test timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	t.Logf("inserting %d jobs...", numJobs)
	jobTypes := []string{
		"load_order", "load_email", "load_notify", "load_report",
		"load_payment", "load_sync", "load_cleanup", "load_fraud",
	}
	jobIDs := make([]string, numJobs)
	for i := range jobIDs {
		jt := jobTypes[i%len(jobTypes)]
		jobIDs[i] = insertJob(t, jt, mrand.Intn(10)+1)
	}

	var processed int64

	handlers := map[string]executor.Handler{}
	for _, jt := range jobTypes {
		name := jt
		handlers[name] = func(ctx context.Context, job *executor.Job) error {
			delay := time.Duration(5+mrand.Intn(20)) * time.Millisecond
			select {
			case <-time.After(delay):
				atomic.AddInt64(&processed, 1)
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}
	}

	t.Logf("starting %d workers...", numWorkers)
	var workerWG sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wid := registerWorker(t, testProjectID)
		concurrency := mrand.Intn(5) + 3 // 3–7
		cfg := &config.Config{
			ProjectID:    testProjectID,
			QueueNames:   []string{"default", "email", "notifications"},
			Concurrency:  concurrency,
			PollInterval: 150 * time.Millisecond,
		}
		exec := executor.New(testPool, nil, wid, &config.Config{Concurrency: concurrency})
		for name, h := range handlers {
			exec.Register(name, h)
		}
		p := poller.New(testPool, nil, exec, wid, cfg)
		if err := p.ResolveQueueIDs(ctx); err != nil {
			t.Logf("worker %d: ResolveQueueIDs failed: %v", w, err)
			continue
		}

		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			pollCtx, pollCancel := context.WithTimeout(ctx, 80*time.Second)
			defer pollCancel()
			p.Run(pollCtx)
		}()
	}

	// Poll until all jobs are in a terminal state
	start := time.Now()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Errorf("load test timed out with %d processed", processed)
			return
		case <-ticker.C:
			var completedCount, deadCount, queuedCount int
			testPool.QueryRow(ctx,
				`SELECT COUNT(*) FROM jobs WHERE id=ANY($1) AND status='completed'`, jobIDs,
			).Scan(&completedCount)
			testPool.QueryRow(ctx,
				`SELECT COUNT(*) FROM jobs WHERE id=ANY($1) AND status='dead'`, jobIDs,
			).Scan(&deadCount)
			testPool.QueryRow(ctx,
				`SELECT COUNT(*) FROM jobs WHERE id=ANY($1) AND status='queued'`, jobIDs,
			).Scan(&queuedCount)
			terminal := completedCount + deadCount
			t.Logf("progress: completed=%d dead=%d queued=%d (elapsed=%v)",
				completedCount, deadCount, queuedCount, time.Since(start).Round(time.Second))
			if terminal >= numJobs {
				t.Logf("load test done in %v: %d completed, %d dead out of %d total",
					time.Since(start).Round(time.Second), completedCount, deadCount, numJobs)
				return
			}
		}
	}
}

var _ = hex.EncodeToString
