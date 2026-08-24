package heartbeat

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/tushar/dis-job-queue/shared/events"
	"github.com/tushar/dis-job-queue/worker/internal/config"
)

type Heartbeater struct {
	db       *pgxpool.Pool
	rdb      *redis.Client
	bus      *events.Publisher
	workerID string
	cfg      *config.Config
}

func New(db *pgxpool.Pool, rdb *redis.Client, bus *events.Publisher, workerID string, cfg *config.Config) *Heartbeater {
	return &Heartbeater{db: db, rdb: rdb, bus: bus, workerID: workerID, cfg: cfg}
}

func (h *Heartbeater) Run(ctx context.Context) {
	ticker := time.NewTicker(h.cfg.HeartbeatInterval)
	defer ticker.Stop()

	h.beat(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.beat(ctx)
		}
	}
}

func (h *Heartbeater) beat(ctx context.Context) {
	var running, completed int

	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM jobs WHERE claimed_by=$1 AND status='running'`, h.workerID,
	).Scan(&running)
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM job_executions je JOIN jobs j ON j.id=je.job_id
		 WHERE j.claimed_by=$1 AND je.status='completed'
		   AND je.started_at > now() - interval '1 hour'`, h.workerID,
	).Scan(&completed)

	if _, err := h.db.Exec(ctx,
		`UPDATE workers SET last_heartbeat_at=now() WHERE id=$1`, h.workerID); err != nil {
		log.Warn().Err(err).Str("worker_id", h.workerID).Msg("heartbeat failed")
		return
	}

	h.db.Exec(ctx,
		`INSERT INTO worker_heartbeats (worker_id, jobs_running, jobs_completed) VALUES ($1,$2,$3)`,
		h.workerID, running, completed)

	if h.bus != nil {
		h.bus.Publish(ctx, events.Event{
			Type: events.WorkerHeartbeat, ProjectID: h.cfg.ProjectID,
			WorkerID: h.workerID, Status: "active",
		})
	}
}
