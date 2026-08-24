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
	if os.Getenv("DATABASE_URL") == "" {
		fmt.Println("SKIP: DATABASE_URL not set - integration tests require a real database")
		os.Exit(0)
	}

	cfg := config.Load()
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
		pool, err = workerdb.NewPool(cfg.DatabaseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "db connect: %v\n", err)
			os.Exit(1)
		}
	}
	testPool = pool

	// Isolated org/project/queue per run so these tests never touch the
	// queues a real dev worker (make dev) is polling against the same DB.
	ctx := context.Background()
	runName := "poller-integration-test-" + workerHex()
	var orgID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO organizations (name, slug) VALUES ($1,$1) RETURNING id`, runName,
	).Scan(&orgID); err != nil {
		fmt.Fprintf(os.Stderr, "create test org: %v\n", err)
		os.Exit(1)
	}
	var projectID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, api_key_hash) VALUES ($1,$2,$2,$2) RETURNING id`,
		orgID, runName,
	).Scan(&projectID); err != nil {
		fmt.Fprintf(os.Stderr, "create test project: %v\n", err)
		os.Exit(1)
	}
	testProjectID = projectID
	var qid string
	if err := pool.QueryRow(ctx,
		`INSERT INTO queues (project_id, name) VALUES ($1,'default') RETURNING id`, projectID,
	).Scan(&qid); err != nil {
		fmt.Fprintf(os.Stderr, "create test queue: %v\n", err)
		os.Exit(1)
	}
	testQueueID = qid

	code := m.Run()
	pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, orgID)
	pool.Close()
	os.Exit(code)
}

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

func TestPriorityOrdering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

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

	exec := executor.New(testPool, nil, nil, workerID, &config.Config{Concurrency: 1})

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
	p := poller.New(testPool, nil, exec, nil, workerID, cfg)
	p.RefreshTopology(ctx)

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

func TestSkipLocked_NoDuplicateClaims(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const numJobs = 20
	jobIDs := make([]string, numJobs)
	for i := range jobIDs {
		jobIDs[i] = insertJob(t, "skip_lock_test", 5)
	}

	var claimed sync.Map
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
		exec := executor.New(testPool, nil, nil, wid, &config.Config{Concurrency: 5})
		exec.Register("skip_lock_test", func(ctx context.Context, job *executor.Job) error {
			if _, loaded := claimed.LoadOrStore(job.ID, wid); loaded {
				atomic.AddInt32(&duplicates, 1)
				t.Errorf("job %s was claimed by two workers (SKIP LOCKED violation)", job.ID)
			}
			return nil
		})

		p := poller.New(testPool, nil, exec, nil, wid, cfg)
		p.RefreshTopology(ctx)

		wg.Add(1)
		go func() {
			defer wg.Done()
			pollCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			p.Run(pollCtx)
		}()
	}

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
		t.Errorf("detected %d double-claimed jobs - FOR UPDATE SKIP LOCKED may not be working", dups)
	}

	count := 0
	claimed.Range(func(_, _ any) bool { count++; return true })
	t.Logf("skip-locked test: %d/%d jobs claimed with no duplicates", count, numJobs)
}

func TestScheduler_PromotesScheduledJobs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

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

	if s := jobStatus(t, jobID); s != "scheduled" {
		t.Fatalf("expected status=scheduled before promotion, got %s", s)
	}

	cfg := &config.Config{
		ProjectID:    testProjectID,
		QueueNames:   []string{"default"},
		Concurrency:  1,
		PollInterval: 100 * time.Millisecond,
	}
	wid := registerWorker(t, testProjectID)
	exec := executor.New(testPool, nil, nil, wid, &config.Config{Concurrency: 1})
	p := poller.New(testPool, nil, exec, nil, wid, cfg)

	schedCtx, schedCancel := context.WithTimeout(ctx, 10*time.Second)
	defer schedCancel()
	go p.RunScheduler(schedCtx)

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

func TestFailure_MovesToDLQ(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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
	exec := executor.New(testPool, nil, nil, wid, &config.Config{Concurrency: 2})
	exec.Register("always_fail_dlq", func(ctx context.Context, job *executor.Job) error {
		return fmt.Errorf("intentional failure for DLQ test")
	})

	p := poller.New(testPool, nil, exec, nil, wid, cfg)
	p.RefreshTopology(ctx)
	pollCtx, pollCancel := context.WithTimeout(ctx, 15*time.Second)
	defer pollCancel()
	go p.Run(pollCtx)

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

	var dlqCount int
	testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM dead_letter_queue WHERE job_id=$1`, jobID,
	).Scan(&dlqCount)
	if dlqCount != 1 {
		t.Errorf("expected 1 DLQ entry, got %d", dlqCount)
	}
	t.Log("job correctly moved to DLQ after exhausting retries")
}

func TestPausedQueue_JobsNotClaimed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

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
	exec := executor.New(testPool, nil, nil, wid, &config.Config{Concurrency: 2})
	exec.Register("paused_queue_test", func(ctx context.Context, job *executor.Job) error {
		return nil
	})

	p := poller.New(testPool, nil, exec, nil, wid, cfg)
	p.RefreshTopology(ctx)

	pollCtx, pollCancel := context.WithTimeout(ctx, 3*time.Second)
	defer pollCancel()
	go p.Run(pollCtx)

	time.Sleep(3 * time.Second)
	status := jobStatus(t, jobID)
	if status != "queued" {
		t.Errorf("paused queue: job should remain 'queued', got %s", status)
	}
	t.Log("paused queue correctly prevents job claiming")
}

func TestLoad_20Workers_200Jobs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	const numJobs = 200
	const numWorkers = 20

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
		concurrency := mrand.Intn(5) + 3
		cfg := &config.Config{
			ProjectID:    testProjectID,
			QueueNames:   []string{"default", "email", "notifications"},
			Concurrency:  concurrency,
			PollInterval: 150 * time.Millisecond,
		}
		exec := executor.New(testPool, nil, nil, wid, &config.Config{Concurrency: concurrency})
		for name, h := range handlers {
			exec.Register(name, h)
		}
		p := poller.New(testPool, nil, exec, nil, wid, cfg)
		if err := p.RefreshTopology(ctx); err != nil {
			t.Logf("worker %d: RefreshTopology failed: %v", w, err)
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
