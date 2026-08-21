package poller

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
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
	cronParser := cron.New(cron.WithSeconds())

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.db.Exec(ctx,
				`UPDATE jobs SET status='queued', updated_at=now()
				 WHERE status='scheduled' AND run_at <= now()`)

			rows, err := p.db.Query(ctx,
				`SELECT id, cron_expression FROM jobs
				 WHERE status IN ('completed','scheduled') AND cron_expression IS NOT NULL AND next_run_at IS NULL`)
			if err != nil {
				continue
			}
			for rows.Next() {
				var id, expr string
				rows.Scan(&id, &expr)
				sched, err := cronParser.AddFunc(expr, func() {})
				if err != nil {
					log.Warn().Str("job_id", id).Str("cron", expr).Msg("invalid cron expression")
					continue
				}
				next := cronParser.Entry(sched).Next
				cronParser.Remove(sched)
				p.db.Exec(ctx,
					`UPDATE jobs SET next_run_at=$1, run_at=$1, status='scheduled', updated_at=now() WHERE id=$2`,
					next, id)
			}
			rows.Close()
		}
	}
}
