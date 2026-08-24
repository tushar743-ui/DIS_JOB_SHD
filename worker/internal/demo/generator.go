package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/tushar/dis-job-queue/shared/events"
	"github.com/tushar/dis-job-queue/shared/lock"
	"github.com/tushar/dis-job-queue/shared/shard"
	"github.com/tushar/dis-job-queue/worker/internal/config"
)

const (
	lockTTL       = 30 * time.Second
	lockRetry     = 10 * time.Second
	pruneInterval = 5 * time.Minute
	failureRate   = 0.08
	scheduledRate = 0.10
	failJobType   = "always_fail"
)

var partitionKeys = []string{"customer-1", "customer-2", "customer-3", "eu-west", "us-east"}

type Generator struct {
	db    *pgxpool.Pool
	rdb   *redis.Client
	bus   *events.Publisher
	cfg   *config.Config
	types []string
}

func New(db *pgxpool.Pool, rdb *redis.Client, bus *events.Publisher, cfg *config.Config, types []string) *Generator {
	return &Generator{db: db, rdb: rdb, bus: bus, cfg: cfg, types: types}
}

func (g *Generator) Run(ctx context.Context) {
	if len(g.types) == 0 {
		log.Warn().Msg("demo generator has no job types to emit, not starting")
		return
	}
	if g.rdb == nil {
		g.loop(ctx)
		return
	}

	lock.Guard(ctx, g.rdb, "djq:demo:"+g.cfg.ProjectID, lock.GuardOptions{
		TTL:           lockTTL,
		RetryInterval: lockRetry,
		OnAcquired: func(l *lock.Lock) {
			log.Info().Int64("fence", l.Fence()).Msg("elected demo traffic generator for this project")
		},
		OnLost: func(err error) {
			log.Warn().Err(err).Msg("demo generator leadership lost, another worker will take over")
		},
		OnUnavailable: func(err error) {
			log.Warn().Err(err).
				Msg("demo generator lock unavailable, standing down (job creation is not idempotent)")
		},
	}, func(held context.Context, _ *lock.Lock) {
		g.loop(held)
	})
}

func (g *Generator) loop(ctx context.Context) {
	ticker := time.NewTicker(g.cfg.DemoInterval)
	defer ticker.Stop()

	pruner := time.NewTicker(pruneInterval)
	defer pruner.Stop()

	g.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.tick(ctx)
		case <-pruner.C:
			g.prune(ctx)
		}
	}
}

func (g *Generator) tick(ctx context.Context) {
	queues, err := g.eligibleQueues(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("demo generator could not resolve queues")
		return
	}
	if len(queues) == 0 {
		return
	}

	for i := 0; i < g.cfg.DemoBurst; i++ {
		queueID := queues[rand.Intn(len(queues))]
		if err := g.enqueue(ctx, queueID); err != nil {
			log.Warn().Err(err).Str("queue_id", queueID).Msg("demo job creation failed")
			return
		}
	}
}

func (g *Generator) eligibleQueues(ctx context.Context) ([]string, error) {
	rows, err := g.db.Query(ctx,
		`SELECT q.id FROM queues q
		 WHERE q.project_id = $1 AND NOT q.paused
		   AND (SELECT count(*) FROM jobs j
		        WHERE j.queue_id = q.id
		          AND j.status IN ('queued','scheduled','claimed','running')) < $2`,
		g.cfg.ProjectID, g.cfg.DemoBacklogMax)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (g *Generator) enqueue(ctx context.Context, queueID string) error {
	jobID := uuid.New().String()
	jobType := g.types[rand.Intn(len(g.types))]
	if rand.Float64() < failureRate {
		jobType = failJobType
	}

	status, runAt := "queued", time.Now()
	if rand.Float64() < scheduledRate {
		status = "scheduled"
		runAt = runAt.Add(time.Duration(10+rand.Intn(50)) * time.Second)
	}

	var partitionKey *string
	if rand.Intn(3) == 0 {
		k := partitionKeys[rand.Intn(len(partitionKeys))]
		partitionKey = &k
	}

	payload, err := json.Marshal(map[string]any{
		"source":     "demo",
		"sequence":   time.Now().UnixNano(),
		"reference":  fmt.Sprintf("REF-%06d", rand.Intn(1000000)),
		"amount_usd": rand.Intn(50000) / 100,
	})
	if err != nil {
		return err
	}

	var inserted string
	err = g.db.QueryRow(ctx,
		`INSERT INTO jobs (id, queue_id, type, payload, status, priority, max_attempts,
		   run_at, timeout_secs, tags, partition_key, shard)
		 SELECT $1::uuid,$2,$3,$4,$5::job_status,$6,$7,$8,$9,$10,$11,
		   `+shard.AssignSQL("$11", "$1")+`
		 FROM queues q WHERE q.id = $2
		 RETURNING id`,
		jobID, queueID, jobType, payload, status,
		1+rand.Intn(10), 1+rand.Intn(3), runAt, 60,
		[]string{"demo"}, partitionKey,
	).Scan(&inserted)
	if err != nil {
		return err
	}

	if status == "queued" {
		g.bus.Publish(ctx, events.Event{
			Type: events.JobEnqueued, ProjectID: g.cfg.ProjectID,
			QueueID: queueID, JobID: jobID, JobType: jobType, Status: status,
		})
	}
	return nil
}

func (g *Generator) prune(ctx context.Context) {
	tag, err := g.db.Exec(ctx,
		`DELETE FROM jobs
		 WHERE queue_id IN (SELECT id FROM queues WHERE project_id = $1)
		   AND status = 'completed'
		   AND 'demo' = ANY(tags)
		   AND completed_at < $2`,
		g.cfg.ProjectID, time.Now().Add(-g.cfg.DemoRetention))
	if err != nil {
		log.Warn().Err(err).Msg("demo retention prune failed")
		return
	}
	if n := tag.RowsAffected(); n > 0 {
		log.Info().Int64("pruned", n).Msg("demo generator pruned expired completed jobs")
	}
}
