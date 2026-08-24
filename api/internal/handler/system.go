package handler

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/tushar/dis-job-queue/api/internal/config"
)

const dependencyProbeTimeout = 2 * time.Second

func Health(db *pgxpool.Pool, rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := contextWithTimeout(r, dependencyProbeTimeout)
		defer cancel()

		checks := map[string]string{"database": "ok", "redis": "ok"}
		status := http.StatusOK

		if err := db.Ping(ctx); err != nil {
			checks["database"] = "unreachable"
			status = http.StatusServiceUnavailable
		}
		if err := rdb.Ping(ctx).Err(); err != nil {
			checks["redis"] = "degraded"
		}

		writeJSON(w, status, map[string]any{
			"ok":     status == http.StatusOK,
			"checks": checks,
		})
	}
}

func Features(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ai_failure_summaries":  cfg.AISummariesEnabled(),
			"ai_summary_model":      cfg.AISummaryModel,
			"live_events":           true,
			"workflow_dependencies": true,
			"queue_sharding":        true,
			"rbac_roles":            []string{"viewer", "member", "admin", "owner"},
		})
	}
}
