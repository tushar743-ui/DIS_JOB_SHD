package poller

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/tushar/dis-job-queue/shared/events"
	"github.com/tushar/dis-job-queue/shared/lock"
	"github.com/tushar/dis-job-queue/shared/shard"
	"github.com/tushar/dis-job-queue/worker/internal/config"
	"github.com/tushar/dis-job-queue/worker/internal/executor"
)

const (
	schedulerInterval  = 5 * time.Second
	schedulerLockTTL   = 30 * time.Second
	schedulerRetry     = 5 * time.Second
	topologyInterval   = 10 * time.Second
	membershipTTL      = 30 * time.Second
	reclaimGracePeriod = 60
)

type topology struct {
	unsharded []string
	sharded   []string
	maxShards int
	owned     []int
}

type Poller struct {
	db       *pgxpool.Pool
	rdb      *redis.Client
	exec     *executor.Executor
	bus      *events.Publisher
	registry *shard.Registry
	workerID string
	cfg      *config.Config

	mu   sync.RWMutex
	topo topology

	kick chan struct{}
}

func New(db *pgxpool.Pool, rdb *redis.Client, exec *executor.Executor, bus *events.Publisher, workerID string, cfg *config.Config) *Poller {
	return &Poller{
		db:       db,
		rdb:      rdb,
		exec:     exec,
		bus:      bus,
		registry: shard.NewRegistry(rdb, cfg.ProjectID, membershipTTL),
		workerID: workerID,
		cfg:      cfg,
		kick:     make(chan struct{}, 1),
	}
}

func (p *Poller) Topology() (unsharded, sharded []string, owned []int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return slices.Clone(p.topo.unsharded), slices.Clone(p.topo.sharded), slices.Clone(p.topo.owned)
}

func (p *Poller) Nudge() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

func (p *Poller) Run(ctx context.Context) {
	p.RefreshTopology(ctx)

	go p.watchTopology(ctx)
	go p.watchEvents(ctx)

	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		case <-p.kick:
			p.poll(ctx)
		}
	}
}

func (p *Poller) watchTopology(ctx context.Context) {
	ticker := time.NewTicker(topologyInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.RefreshTopology(ctx)
		}
	}
}

func (p *Poller) watchEvents(ctx context.Context) {
	if p.rdb == nil {
		return
	}
	stream := events.Subscribe(ctx, p.rdb, p.cfg.ProjectID)
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-stream:
			if !ok {
				return
			}
			if evt.Type == events.QueuePaused || evt.Type == events.QueueResumed {
				p.RefreshTopology(ctx)
			}
			if evt.Wakes() {
				p.Nudge()
			}
		}
	}
}

func (p *Poller) RefreshTopology(ctx context.Context) error {
	query := `SELECT id, shard_count FROM queues WHERE project_id=$1 AND paused=false`
	args := []any{p.cfg.ProjectID}
	if len(p.cfg.QueueNames) > 0 {
		query += ` AND name=ANY($2)`
		args = append(args, p.cfg.QueueNames)
	}
	rows, err := p.db.Query(ctx, query, args...)
	if err != nil {
		log.Error().Err(err).Msg("failed to resolve queues")
		return err
	}

	next := topology{unsharded: []string{}, sharded: []string{}}
	for rows.Next() {
		var id string
		var shardCount int
		if err := rows.Scan(&id, &shardCount); err != nil {
			log.Error().Err(err).Msg("queue scan error")
			continue
		}
		if shardCount <= 1 {
			next.unsharded = append(next.unsharded, id)
			continue
		}
		next.sharded = append(next.sharded, id)
		next.maxShards = max(next.maxShards, shardCount)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("failed to read queues")
		return err
	}

	next.owned = p.ownedShards(ctx, next.maxShards)

	p.mu.Lock()
	changed := !slices.Equal(p.topo.owned, next.owned) ||
		!slices.Equal(p.topo.sharded, next.sharded) ||
		!slices.Equal(p.topo.unsharded, next.unsharded)
	p.topo = next
	p.mu.Unlock()

	if changed {
		log.Info().
			Int("unsharded_queues", len(next.unsharded)).
			Int("sharded_queues", len(next.sharded)).
			Ints("owned_shards", next.owned).
			Int("shard_space", next.maxShards).
			Msg("queue topology updated")
	}
	return nil
}

func (p *Poller) ownedShards(ctx context.Context, maxShards int) []int {
	if maxShards == 0 {
		return nil
	}
	if p.rdb == nil {
		return shard.All(maxShards)
	}

	members, err := p.registry.Heartbeat(ctx, p.workerID)
	if err != nil {
		log.Warn().Err(err).
			Msg("shard membership registry unavailable, claiming every shard until it recovers")
		return shard.All(maxShards)
	}
	return shard.Owned(p.workerID, members, maxShards)
}

const claimSQL = `
	UPDATE jobs SET
	  status = 'claimed',
	  claimed_by = $1,
	  claimed_at = now(),
	  updated_at = now()
	WHERE id IN (
	  SELECT j.id FROM jobs j
	  WHERE (j.queue_id = ANY($2) OR (j.queue_id = ANY($3) AND j.shard = ANY($4)))
	    AND j.status = 'queued'
	    AND j.run_at <= now()
	    AND NOT EXISTS (
	      SELECT 1 FROM job_dependencies d
	      JOIN jobs parent ON parent.id = d.depends_on_job_id
	      WHERE d.job_id = j.id AND parent.status <> 'completed'
	    )
	  ORDER BY j.priority DESC, j.run_at ASC
	  LIMIT $5
	  FOR UPDATE SKIP LOCKED
	)
	RETURNING id, queue_id, type, payload, attempt_count, max_attempts, timeout_secs, cron_expression`

func (p *Poller) poll(ctx context.Context) {
	unsharded, sharded, owned := p.Topology()
	if len(unsharded) == 0 && len(sharded) == 0 {
		return
	}
	if len(sharded) > 0 && len(owned) == 0 && len(unsharded) == 0 {
		return
	}

	freeSlots := p.exec.FreeSlots()
	if freeSlots <= 0 {
		return
	}

	rows, err := p.db.Query(ctx, claimSQL, p.workerID, unsharded, sharded, owned, freeSlots)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Error().Err(err).Msg("poll error")
		}
		return
	}

	claimed := make([]*executor.Job, 0, freeSlots)
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
		claimed = append(claimed, &job)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("claim read error")
	}

	policies := map[string]*executor.RetryPolicy{}
	for _, job := range claimed {
		policy, cached := policies[job.QueueID]
		if !cached {
			policy = p.fetchRetryPolicy(ctx, job.QueueID)
			policies[job.QueueID] = policy
		}
		job.RetryPolicy = policy

		log.Debug().Str("job_id", job.ID).Str("type", job.Type).Msg("claimed job")
		p.exec.Submit(job)
	}

	if len(claimed) == freeSlots {
		p.Nudge()
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

func (p *Poller) RunScheduler(ctx context.Context) {
	if p.rdb == nil {
		p.schedulerLoop(ctx)
		return
	}

	lock.Guard(ctx, p.rdb, "djq:scheduler:"+p.cfg.ProjectID, lock.GuardOptions{
		TTL:           schedulerLockTTL,
		RetryInterval: schedulerRetry,
		OnAcquired: func(l *lock.Lock) {
			log.Info().Str("worker_id", p.workerID).Int64("fence", l.Fence()).
				Msg("elected scheduler for this project")
		},
		OnLost: func(err error) {
			log.Warn().Err(err).Msg("scheduler leadership lost, another worker will take over")
		},
		OnUnavailable: func(err error) {
			log.Warn().Err(err).
				Msg("scheduler lock unavailable, running sweep unguarded (sweeps are idempotent)")
			p.sweep(ctx)
		},
	}, func(held context.Context, _ *lock.Lock) {
		p.schedulerLoop(held)
	})
}

func (p *Poller) schedulerLoop(ctx context.Context) {
	ticker := time.NewTicker(schedulerInterval)
	defer ticker.Stop()

	p.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.sweep(ctx)
		}
	}
}

func (p *Poller) sweep(ctx context.Context) {
	p.promoteDueJobs(ctx)
	p.scheduleCronJobs(ctx)
	p.unblockSatisfiedJobs(ctx)
	p.cancelUnsatisfiableJobs(ctx)
	p.reclaimStuckJobs(ctx)
}

const projectQueues = `(SELECT id FROM queues WHERE project_id = $1)`

func (p *Poller) promoteDueJobs(ctx context.Context) {
	tag, err := p.db.Exec(ctx,
		`UPDATE jobs SET status='queued', updated_at=now()
		 WHERE id IN (
		   SELECT id FROM jobs
		   WHERE status='scheduled' AND run_at <= now()
		     AND queue_id IN `+projectQueues+`
		   FOR UPDATE SKIP LOCKED
		 )`, p.cfg.ProjectID)
	if err != nil {
		log.Error().Err(err).Msg("failed to promote scheduled jobs")
		return
	}
	if n := tag.RowsAffected(); n > 0 {
		log.Debug().Int64("jobs", n).Msg("promoted scheduled jobs")
		p.publish(ctx, events.Event{Type: events.JobEnqueued, ProjectID: p.cfg.ProjectID})
		p.Nudge()
	}
}

func (p *Poller) unblockSatisfiedJobs(ctx context.Context) {
	rows, err := p.db.Query(ctx,
		`UPDATE jobs SET status='queued', run_at=now(), updated_at=now()
		 WHERE id IN (
		   SELECT j.id FROM jobs j
		   WHERE j.status='blocked'
		     AND j.queue_id IN `+projectQueues+`
		     AND NOT EXISTS (
		       SELECT 1 FROM job_dependencies d
		       JOIN jobs parent ON parent.id = d.depends_on_job_id
		       WHERE d.job_id = j.id AND parent.status <> 'completed'
		     )
		   FOR UPDATE SKIP LOCKED
		 )
		 RETURNING id, queue_id, type`, p.cfg.ProjectID)
	if err != nil {
		log.Error().Err(err).Msg("failed to unblock satisfied jobs")
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var jobID, queueID, jobType string
		if err := rows.Scan(&jobID, &queueID, &jobType); err != nil {
			continue
		}
		count++
		p.publish(ctx, events.Event{
			Type: events.JobUnblocked, ProjectID: p.cfg.ProjectID,
			QueueID: queueID, JobID: jobID, JobType: jobType, Status: "queued",
		})
	}
	if count > 0 {
		log.Info().Int("jobs", count).Msg("unblocked jobs whose dependencies completed")
		p.Nudge()
	}
}

func (p *Poller) cancelUnsatisfiableJobs(ctx context.Context) {
	rows, err := p.db.Query(ctx,
		`UPDATE jobs SET status='cancelled', updated_at=now(), completed_at=now(),
		   last_error='upstream dependency failed permanently, this job can never run'
		 WHERE id IN (
		   SELECT j.id FROM jobs j
		   WHERE j.status='blocked'
		     AND j.queue_id IN `+projectQueues+`
		     AND EXISTS (
		       SELECT 1 FROM job_dependencies d
		       JOIN jobs parent ON parent.id = d.depends_on_job_id
		       WHERE d.job_id = j.id AND parent.status IN ('dead','cancelled')
		     )
		   FOR UPDATE SKIP LOCKED
		 )
		 RETURNING id, queue_id, type`, p.cfg.ProjectID)
	if err != nil {
		log.Error().Err(err).Msg("failed to cancel unsatisfiable jobs")
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var jobID, queueID, jobType string
		if err := rows.Scan(&jobID, &queueID, &jobType); err != nil {
			continue
		}
		count++
		p.publish(ctx, events.Event{
			Type: events.JobCancelled, ProjectID: p.cfg.ProjectID,
			QueueID: queueID, JobID: jobID, JobType: jobType, Status: "cancelled",
		})
	}
	if count > 0 {
		log.Warn().Int("jobs", count).Msg("cancelled jobs whose dependencies failed permanently")
	}
}

func (p *Poller) scheduleCronJobs(ctx context.Context) {
	rows, err := p.db.Query(ctx,
		`SELECT id, cron_expression FROM jobs
		 WHERE status IN ('completed','scheduled') AND cron_expression IS NOT NULL
		   AND next_run_at IS NULL AND queue_id IN `+projectQueues, p.cfg.ProjectID)
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

func (p *Poller) reclaimStuckJobs(ctx context.Context) {
	tag, err := p.db.Exec(ctx,
		`UPDATE jobs SET status='queued', claimed_by=NULL, claimed_at=NULL,
		  run_at=now(), updated_at=now()
		 WHERE claimed_at IS NOT NULL
		   AND queue_id IN `+projectQueues+`
		   AND (
		     (status='claimed' AND claimed_at < now() - make_interval(secs => $2))
		     OR (status='running' AND claimed_at < now() - make_interval(secs => timeout_secs + $2))
		   )`, p.cfg.ProjectID, reclaimGracePeriod)
	if err != nil {
		log.Error().Err(err).Msg("failed to reclaim stuck jobs")
		return
	}
	if n := tag.RowsAffected(); n > 0 {
		log.Warn().Int64("jobs", n).Msg("reclaimed stuck jobs")
		p.publish(ctx, events.Event{Type: events.JobEnqueued, ProjectID: p.cfg.ProjectID})
		p.Nudge()
	}
}

func (p *Poller) publish(ctx context.Context, e events.Event) {
	if p.bus != nil {
		p.bus.Publish(ctx, e)
	}
}

func (p *Poller) Deregister(ctx context.Context) {
	if p.rdb == nil {
		return
	}
	if err := p.registry.Leave(ctx, p.workerID); err != nil {
		log.Warn().Err(err).Msg("failed to leave shard membership registry")
	}
}
