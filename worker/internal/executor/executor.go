package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/tushar/dis-job-queue/worker/internal/config"
)

// Job represents a claimed job ready for execution.
type Job struct {
	ID             string
	QueueID        string
	Type           string
	Payload        json.RawMessage
	AttemptCount   int
	MaxAttempts    int
	TimeoutSecs    int
	CronExpression *string
	RetryPolicy    *RetryPolicy
}

type RetryPolicy struct {
	Strategy       string  // fixed | linear | exponential
	MaxAttempts    int
	InitialDelayMs int
	MaxDelayMs     int
	Multiplier     float64
}

// Handler is a function that processes a job payload.
type Handler func(ctx context.Context, job *Job) error

// Executor runs jobs concurrently with a semaphore.
type Executor struct {
	db       *pgxpool.Pool
	rdb      *redis.Client
	workerID string
	cfg      *config.Config
	handlers map[string]Handler
	sem      chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex
}

func New(db *pgxpool.Pool, rdb *redis.Client, workerID string, cfg *config.Config) *Executor {
	return &Executor{
		db:       db,
		rdb:      rdb,
		workerID: workerID,
		cfg:      cfg,
		handlers: map[string]Handler{},
		sem:      make(chan struct{}, cfg.Concurrency),
	}
}

// Register adds a handler for a job type. Workers typically register handlers at startup.
func (e *Executor) Register(jobType string, h Handler) {
	e.mu.Lock()
	e.handlers[jobType] = h
	e.mu.Unlock()
}

// Submit dispatches a job for async execution.
func (e *Executor) Submit(job *Job) {
	e.sem <- struct{}{}
	e.wg.Add(1)
	go func() {
		defer func() {
			<-e.sem
			e.wg.Done()
		}()
		e.run(job)
	}()
}

// SemChan exposes the semaphore so the poller can check free capacity.
func (e *Executor) SemChan() chan struct{} { return e.sem }

// Drain waits for all running jobs to finish (or ctx to expire).
func (e *Executor) Drain(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		log.Warn().Msg("drain timeout — some jobs may not have finished")
	}
}

func (e *Executor) run(job *Job) {
	timeout := time.Duration(job.TimeoutSecs) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	execID, err := e.startExecution(ctx, job)
	if err != nil {
		log.Error().Err(err).Str("job_id", job.ID).Msg("failed to start execution record")
		return
	}

	start := time.Now()
	var execErr error

	e.mu.Lock()
	h, ok := e.handlers[job.Type]
	e.mu.Unlock()

	if !ok {
		execErr = fmt.Errorf("no handler registered for job type %q", job.Type)
	} else {
		func() {
			defer func() {
				if r := recover(); r != nil {
					execErr = fmt.Errorf("panic: %v", r)
				}
			}()
			execErr = h(ctx, job)
		}()
	}

	durationMs := int(time.Since(start).Milliseconds())

	if execErr != nil {
		e.handleFailure(ctx, job, execID, execErr, durationMs)
	} else {
		e.handleSuccess(ctx, job, execID, durationMs)
	}
}

func (e *Executor) startExecution(ctx context.Context, job *Job) (string, error) {
	var execID string
	err := e.db.QueryRow(ctx,
		`INSERT INTO job_executions (job_id, worker_id, attempt_number, status)
		 VALUES ($1,$2,$3,'running') RETURNING id`,
		job.ID, e.workerID, job.AttemptCount+1,
	).Scan(&execID)
	if err != nil {
		return "", err
	}
	e.db.Exec(ctx,
		`UPDATE jobs SET status='running', attempt_count=attempt_count+1,
		  claimed_by=$1, claimed_at=now(), updated_at=now() WHERE id=$2`,
		e.workerID, job.ID)
	return execID, nil
}

func (e *Executor) handleSuccess(ctx context.Context, job *Job, execID string, durationMs int) {
	e.db.Exec(ctx,
		`UPDATE job_executions SET status='completed', completed_at=now(), duration_ms=$1 WHERE id=$2`,
		durationMs, execID)

	// Handle recurring cron jobs — re-schedule next run
	if job.CronExpression != nil {
		e.db.Exec(ctx,
			`UPDATE jobs SET status='scheduled', attempt_count=0, last_error=NULL,
			  completed_at=now(), updated_at=now()
			 WHERE id=$1`,
			job.ID)
		// next_run_at is updated by the scheduler loop
	} else {
		e.db.Exec(ctx,
			`UPDATE jobs SET status='completed', completed_at=now(), updated_at=now(),
			  claimed_by=NULL WHERE id=$1`, job.ID)
	}

	e.writeLog(context.Background(), job.ID, execID, "info", "job completed", nil)
	log.Info().Str("job_id", job.ID).Str("type", job.Type).Int("duration_ms", durationMs).Msg("job completed")
}

func (e *Executor) handleFailure(ctx context.Context, job *Job, execID string, execErr error, durationMs int) {
	errMsg := execErr.Error()
	e.db.Exec(ctx,
		`UPDATE job_executions SET status='failed', completed_at=now(), duration_ms=$1, error_message=$2 WHERE id=$3`,
		durationMs, errMsg, execID)

	nextAttempt := job.AttemptCount + 1
	if nextAttempt >= job.MaxAttempts {
		// Move to DLQ
		e.db.Exec(ctx,
			`UPDATE jobs SET status='dead', last_error=$1, updated_at=now(), claimed_by=NULL WHERE id=$2`,
			errMsg, job.ID)
		e.db.Exec(ctx,
			`INSERT INTO dead_letter_queue (job_id, queue_id, final_error, attempts)
			 VALUES ($1,$2,$3,$4) ON CONFLICT (job_id) DO NOTHING`,
			job.ID, job.QueueID, errMsg, nextAttempt)
		log.Warn().Str("job_id", job.ID).Str("error", errMsg).Msg("job moved to DLQ")
	} else {
		// Re-queue with backoff
		delay := e.calcDelay(job, nextAttempt)
		runAt := time.Now().Add(delay)
		e.db.Exec(ctx,
			`UPDATE jobs SET status='queued', last_error=$1, run_at=$2, updated_at=now(), claimed_by=NULL WHERE id=$3`,
			errMsg, runAt, job.ID)
		log.Warn().Str("job_id", job.ID).Err(execErr).
			Dur("retry_delay", delay).Int("attempt", nextAttempt).Msg("job failed, will retry")
	}
	e.writeLog(context.Background(), job.ID, execID, "error", errMsg, nil)
}

func (e *Executor) calcDelay(job *Job, attempt int) time.Duration {
	if job.RetryPolicy == nil {
		// Default exponential
		delay := time.Second * time.Duration(1<<uint(attempt-1))
		if delay > 5*time.Minute {
			delay = 5 * time.Minute
		}
		return delay
	}
	rp := job.RetryPolicy
	switch rp.Strategy {
	case "fixed":
		return time.Duration(rp.InitialDelayMs) * time.Millisecond
	case "linear":
		d := time.Duration(rp.InitialDelayMs*attempt) * time.Millisecond
		if int(d.Milliseconds()) > rp.MaxDelayMs {
			d = time.Duration(rp.MaxDelayMs) * time.Millisecond
		}
		return d
	default: // exponential
		d := float64(rp.InitialDelayMs)
		for i := 1; i < attempt; i++ {
			d *= rp.Multiplier
		}
		if d > float64(rp.MaxDelayMs) {
			d = float64(rp.MaxDelayMs)
		}
		return time.Duration(d) * time.Millisecond
	}
}

func (e *Executor) writeLog(ctx context.Context, jobID, execID, level, msg string, meta map[string]any) {
	metaBytes := []byte("{}")
	if meta != nil {
		metaBytes, _ = json.Marshal(meta)
	}
	e.db.Exec(ctx,
		`INSERT INTO job_logs (job_id, execution_id, level, message, metadata) VALUES ($1,$2,$3,$4,$5)`,
		jobID, execID, level, msg, metaBytes)
}
