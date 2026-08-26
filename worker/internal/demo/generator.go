package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

type step struct {
	jobType   string
	stage     string
	dependsOn []int
}

type pipeline struct {
	name  string
	steps []step
}

var pipelines = []pipeline{
	{
		name: "checkout",
		steps: []step{
			{jobType: "process_payment", stage: "authorize"},
			{jobType: "fraud_check", stage: "screen", dependsOn: []int{0}},
			{jobType: "sync_inventory", stage: "reserve-stock", dependsOn: []int{0}},
			{jobType: "process_order", stage: "fulfil", dependsOn: []int{1, 2}},
			{jobType: "send_email", stage: "confirm", dependsOn: []int{3}},
		},
	},
	{
		name: "nightly_etl",
		steps: []step{
			{jobType: "etl_batch", stage: "extract"},
			{jobType: "generate_report", stage: "aggregate", dependsOn: []int{0}},
			{jobType: "cleanup_temp_files", stage: "sweep", dependsOn: []int{0}},
			{jobType: "notify", stage: "announce", dependsOn: []int{1, 2}},
		},
	},
	{
		name: "media_pipeline",
		steps: []step{
			{jobType: "transcode_video", stage: "encode"},
			{jobType: "generate_report", stage: "index", dependsOn: []int{0}},
			{jobType: "push_notification", stage: "alert-subscribers", dependsOn: []int{0}},
			{jobType: "send_sms", stage: "alert-owner", dependsOn: []int{1}},
		},
	},
	{
		name: "order_notifications",
		steps: []step{
			{jobType: "process_order", stage: "accept"},
			{jobType: "send_email", stage: "email-receipt", dependsOn: []int{0}},
			{jobType: "push_notification", stage: "push-receipt", dependsOn: []int{0}},
		},
	},
	{
		name: "inventory_sync",
		steps: []step{
			{jobType: "sync_inventory", stage: "pull"},
			{jobType: "notify", stage: "publish", dependsOn: []int{0}},
		},
	},
}

type Generator struct {
	db        *pgxpool.Pool
	rdb       *redis.Client
	bus       *events.Publisher
	cfg       *config.Config
	types     map[string]bool
	templates []pipeline
}

func New(db *pgxpool.Pool, rdb *redis.Client, bus *events.Publisher, cfg *config.Config, types []string) *Generator {
	known := make(map[string]bool, len(types))
	for _, t := range types {
		known[t] = true
	}

	usable := make([]pipeline, 0, len(pipelines))
	for _, p := range pipelines {
		supported := true
		for _, s := range p.steps {
			if !known[s.jobType] {
				supported = false
				break
			}
		}
		if supported {
			usable = append(usable, p)
		}
	}

	return &Generator{db: db, rdb: rdb, bus: bus, cfg: cfg, types: known, templates: usable}
}

func (g *Generator) Run(ctx context.Context) {
	if len(g.templates) == 0 {
		log.Warn().Msg("demo generator has no runnable pipelines, not starting")
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
		if err := g.enqueueWorkflow(ctx, queueID); err != nil {
			log.Warn().Err(err).Str("queue_id", queueID).Msg("demo workflow creation failed")
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
		          AND j.status IN ('queued','scheduled','claimed','running','blocked')) < $2`,
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

func leafSteps(p pipeline) []int {
	hasDependent := make([]bool, len(p.steps))
	for _, s := range p.steps {
		for _, d := range s.dependsOn {
			hasDependent[d] = true
		}
	}
	leaves := []int{}
	for i, dependent := range hasDependent {
		if !dependent {
			leaves = append(leaves, i)
		}
	}
	return leaves
}

func (g *Generator) enqueueWorkflow(ctx context.Context, queueID string) error {
	p := g.templates[rand.Intn(len(g.templates))]
	batchID := uuid.New().String()

	ids := make([]string, len(p.steps))
	for i := range ids {
		ids[i] = uuid.New().String()
	}

	types := make([]string, len(p.steps))
	for i, s := range p.steps {
		types[i] = s.jobType
	}
	if g.types[failJobType] && rand.Float64() < failureRate {
		leaves := leafSteps(p)
		types[leaves[rand.Intn(len(leaves))]] = failJobType
	}

	var partitionKey *string
	if rand.Intn(3) == 0 {
		k := partitionKeys[rand.Intn(len(partitionKeys))]
		partitionKey = &k
	}

	rootRunAt := time.Now()
	rootStatus := "queued"
	if rand.Float64() < scheduledRate {
		rootStatus = "scheduled"
		rootRunAt = rootRunAt.Add(time.Duration(10+rand.Intn(50)) * time.Second)
	}

	tx, err := g.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	published := make([]int, 0, len(p.steps))

	for i, s := range p.steps {
		status, runAt := rootStatus, rootRunAt
		if len(s.dependsOn) > 0 {
			status, runAt = "blocked", time.Now()
		}

		payload, err := json.Marshal(map[string]any{
			"source":     "demo",
			"workflow":   p.name,
			"stage":      s.stage,
			"step":       fmt.Sprintf("%d/%d", i+1, len(p.steps)),
			"reference":  fmt.Sprintf("REF-%06d", rand.Intn(1000000)),
			"amount_usd": rand.Intn(50000) / 100,
		})
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO jobs (id, queue_id, type, payload, status, priority, max_attempts,
			   run_at, timeout_secs, batch_id, tags, partition_key, shard)
			 SELECT $1::uuid,$2,$3,$4,$5::job_status,$6,$7,$8,$9,$10,$11,$12,
			   `+shard.AssignSQL("$12", "$1")+`
			 FROM queues q WHERE q.id = $2`,
			ids[i], queueID, types[i], payload, status,
			len(p.steps)-i, 1+rand.Intn(3), runAt, 60, batchID,
			[]string{"demo", "workflow:" + p.name}, partitionKey,
		); err != nil {
			return err
		}

		if len(s.dependsOn) > 0 {
			edges := make([][]any, 0, len(s.dependsOn))
			for _, d := range s.dependsOn {
				edges = append(edges, []any{ids[i], ids[d]})
			}
			if _, err := tx.CopyFrom(ctx,
				pgx.Identifier{"job_dependencies"},
				[]string{"job_id", "depends_on_job_id"},
				pgx.CopyFromRows(edges),
			); err != nil {
				return err
			}
			continue
		}

		if status == "queued" {
			published = append(published, i)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	for _, i := range published {
		g.bus.Publish(ctx, events.Event{
			Type: events.JobEnqueued, ProjectID: g.cfg.ProjectID,
			QueueID: queueID, JobID: ids[i], JobType: types[i], Status: "queued",
		})
	}
	return nil
}

func (g *Generator) prune(ctx context.Context) {
	cutoff := time.Now().Add(-g.cfg.DemoRetention)

	tag, err := g.db.Exec(ctx,
		`DELETE FROM jobs
		 WHERE queue_id IN (SELECT id FROM queues WHERE project_id = $1)
		   AND 'demo' = ANY(tags)
		   AND batch_id IN (
		     SELECT batch_id FROM jobs
		     WHERE queue_id IN (SELECT id FROM queues WHERE project_id = $1)
		       AND 'demo' = ANY(tags) AND batch_id IS NOT NULL
		     GROUP BY batch_id
		     HAVING bool_and(status = 'completed') AND max(completed_at) < $2
		   )`,
		g.cfg.ProjectID, cutoff)
	if err != nil {
		log.Warn().Err(err).Msg("demo retention prune failed")
		return
	}

	legacy, err := g.db.Exec(ctx,
		`DELETE FROM jobs
		 WHERE queue_id IN (SELECT id FROM queues WHERE project_id = $1)
		   AND status = 'completed'
		   AND batch_id IS NULL
		   AND 'demo' = ANY(tags)
		   AND completed_at < $2`,
		g.cfg.ProjectID, cutoff)
	if err != nil {
		log.Warn().Err(err).Msg("demo retention prune of standalone jobs failed")
		return
	}

	if n := tag.RowsAffected() + legacy.RowsAffected(); n > 0 {
		log.Info().Int64("pruned", n).Msg("demo generator pruned expired completed workflows")
	}
}
