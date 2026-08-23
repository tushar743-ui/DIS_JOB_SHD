package poller

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/tushar/dis-job-queue/worker/internal/config"
	"github.com/tushar/dis-job-queue/worker/internal/executor"
)

type Poller struct {
	db       *pgxpool.Pool
	rdb      *redis.Client
	exec     *executor.Executor
	workerID string
	cfg      *config.Config
	queueIDs []string
}

func New(db *pgxpool.Pool, rdb *redis.Client, exec *executor.Executor, workerID string, cfg *config.Config) *Poller {
	return &Poller{
		db:       db,
		rdb:      rdb,
		exec:     exec,
		workerID: workerID,
		cfg:      cfg,
	}
}

func (p *Poller) ResolveQueueIDs(ctx context.Context) error {
	rows, err := p.db.Query(ctx,
		`SELECT id FROM queues WHERE project_id=$1 AND name=ANY($2) AND paused=false`,
		p.cfg.ProjectID, p.cfg.QueueNames)
	if err != nil {
		return err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	p.queueIDs = ids
	return nil
}

func (p *Poller) Run(ctx context.Context) {
	if err := p.ResolveQueueIDs(ctx); err != nil {
		log.Error().Err(err).Msg("failed to resolve queue IDs")
	}

	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *Poller) poll(ctx context.Context) {
	if len(p.queueIDs) == 0 {
		p.ResolveQueueIDs(ctx)
		return
	}

	freeSlots := cap(p.sem()) - len(p.sem())
	if freeSlots <= 0 {
		return
	}

	rows, err := p.db.Query(ctx, `
		UPDATE jobs SET
		  status = 'claimed',
		  claimed_by = $1,
		  claimed_at = now(),
		  updated_at = now()
		WHERE id IN (
		  SELECT id FROM jobs
		  WHERE queue_id = ANY($2)
		    AND status = 'queued'
		    AND run_at <= now()
		  ORDER BY priority DESC, run_at ASC
		  LIMIT $3
		  FOR UPDATE SKIP LOCKED
		)
		RETURNING id, queue_id, type, payload, attempt_count, max_attempts, timeout_secs, cron_expression`,
		p.workerID, p.queueIDs, freeSlots,
	)
	if err != nil {
		log.Error().Err(err).Msg("poll error")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var job executor.Job
		var payloadBytes []byte
		var cronExpr *string
		if err := rows.Scan(&job.ID, &job.QueueID, &job.Type, &payloadBytes,
			&job.AttemptCount, &job.MaxAttempts, &job.TimeoutSecs, &cronExpr); err != nil {
			log.Error().Err(err).Msg("scan error")
			continue
		}
		job.Payload = json.RawMessage(payloadBytes)
		job.CronExpression = cronExpr

		job.RetryPolicy = p.fetchRetryPolicy(ctx, job.QueueID)

		log.Debug().Str("job_id", job.ID).Str("type", job.Type).Msg("claimed job")
		p.exec.Submit(&job)
	}
}

func (p *Poller) fetchRetryPolicy(ctx context.Context, queueID string) *executor.RetryPolicy {
	var rp executor.RetryPolicy
	err := p.db.QueryRow(ctx,
		`SELECT rp.strategy, rp.max_attempts, rp.initial_delay_ms, rp.max_delay_ms, rp.multiplier
		 FROM queues q JOIN retry_policies rp ON rp.id=q.retry_policy_id
		 WHERE q.id=$1`, queueID,
	).Scan(&rp.Strategy, &rp.MaxAttempts, &rp.InitialDelayMs, &rp.MaxDelayMs, &rp.Multiplier)
	if err != nil {
		return nil
	}
	return &rp
}

func (p *Poller) sem() chan struct{} {
	return p.exec.SemChan()
}

func (p *Poller) RunScheduler(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.promoteDueJobs(ctx)
			p.scheduleCronJobs(ctx)
			p.reclaimStuckJobs(ctx)
		}
	}
}

// promoteDueJobs moves scheduled jobs whose run_at has passed into the queue.
func (p *Poller) promoteDueJobs(ctx context.Context) {
	// FOR UPDATE SKIP LOCKED prevents multiple workers from double-promoting
	if _, err := p.db.Exec(ctx,
		`UPDATE jobs SET status='queued', updated_at=now()
		 WHERE id IN (
		   SELECT id FROM jobs
		   WHERE status='scheduled' AND run_at <= now()
		   FOR UPDATE SKIP LOCKED
		 )`); err != nil {
		log.Error().Err(err).Msg("failed to promote scheduled jobs")
	}
}

// scheduleCronJobs gives every recurring job that has no pending run a next fire time.
func (p *Poller) scheduleCronJobs(ctx context.Context) {
	rows, err := p.db.Query(ctx,
		`SELECT id, cron_expression FROM jobs
		 WHERE status IN ('completed','scheduled') AND cron_expression IS NOT NULL AND next_run_at IS NULL
		 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		log.Error().Err(err).Msg("failed to list cron jobs")
		return
	}

	type pending struct{ id, expr string }
	due := []pending{}
	for rows.Next() {
		var pj pending
		if err := rows.Scan(&pj.id, &pj.expr); err != nil {
			log.Error().Err(err).Msg("cron scan error")
			continue
		}
		due = append(due, pj)
	}
	rows.Close()

	now := time.Now()
	for _, pj := range due {
		next, err := executor.NextCronRun(pj.expr, now)
		if err != nil {
			log.Warn().Str("job_id", pj.id).Str("cron", pj.expr).Msg("invalid cron expression")
			// Park it so the scheduler stops re-reading a job it can never schedule.
			p.db.Exec(ctx,
				`UPDATE jobs SET status='failed', last_error=$1, updated_at=now() WHERE id=$2`,
				"invalid cron expression: "+pj.expr, pj.id)
			continue
		}
		if _, err := p.db.Exec(ctx,
			`UPDATE jobs SET next_run_at=$1, run_at=$1, status='scheduled', updated_at=now() WHERE id=$2`,
			next, pj.id); err != nil {
			log.Error().Err(err).Str("job_id", pj.id).Msg("failed to schedule next cron run")
			continue
		}
		log.Debug().Str("job_id", pj.id).Time("next_run_at", next).Msg("cron job scheduled")
	}
}

// reclaimStuckJobs re-queues jobs whose worker died between claiming and finishing.
// A claim that never turned into a run is given a short grace period; a job that is
// actually running gets its own timeout plus a minute before it is taken back.
func (p *Poller) reclaimStuckJobs(ctx context.Context) {
	tag, err := p.db.Exec(ctx,
		`UPDATE jobs SET status='queued', claimed_by=NULL, claimed_at=NULL,
		  run_at=now(), updated_at=now()
		 WHERE claimed_at IS NOT NULL
		   AND (
		     (status='claimed' AND claimed_at < now() - interval '60 seconds')
		     OR (status='running' AND claimed_at < now() - make_interval(secs => timeout_secs + 60))
		   )`)
	if err != nil {
		log.Error().Err(err).Msg("failed to reclaim stuck jobs")
		return
	}
	if n := tag.RowsAffected(); n > 0 {
		log.Warn().Int64("jobs", n).Msg("reclaimed stuck jobs")
	}
}
