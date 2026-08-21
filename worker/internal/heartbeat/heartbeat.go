package heartbeat

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

type Heartbeater struct {
	db       *pgxpool.Pool
	workerID string
	interval time.Duration
}

func New(db *pgxpool.Pool, workerID string) *Heartbeater {
	return &Heartbeater{db: db, workerID: workerID, interval: 10 * time.Second}
}

func (h *Heartbeater) Run(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

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

	_, err := h.db.Exec(ctx,
		`UPDATE workers SET last_heartbeat_at=now() WHERE id=$1`, h.workerID)
	if err != nil {
		log.Warn().Err(err).Str("worker_id", h.workerID).Msg("heartbeat failed")
		return
	}

	h.db.Exec(ctx,
		`INSERT INTO worker_heartbeats (worker_id, jobs_running, jobs_completed) VALUES ($1,$2,$3)`,
		h.workerID, running, completed)
}
