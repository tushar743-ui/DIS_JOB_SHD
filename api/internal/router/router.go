package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/tushar/dis-job-queue/api/internal/config"
	"github.com/tushar/dis-job-queue/api/internal/handler"
	"github.com/tushar/dis-job-queue/api/internal/middleware"
)

func New(cfg *config.Config, pool *pgxpool.Pool, rdb *redis.Client) http.Handler {
	r := chi.NewRouter()

	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.CORS)
	r.Use(middleware.RateLimiter(rdb, 200, 60*1_000_000_000))

	authH := handler.NewAuthHandler(pool, cfg)
	orgH := handler.NewOrgHandler(pool)
	projectH := handler.NewProjectHandler(pool)
	queueH := handler.NewQueueHandler(pool)
	jobH := handler.NewJobHandler(pool)
	workerH := handler.NewWorkerHandler(pool)
	dlqH := handler.NewDLQHandler(pool)
	metricsH := handler.NewMetricsHandler(pool)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/register", authH.Register)
		r.Post("/auth/login", authH.Login)
		r.Post("/auth/refresh", authH.Refresh)

		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWTSecret))

			r.Post("/auth/logout", authH.Logout)
			r.Get("/auth/me", authH.Me)

			r.Get("/orgs", orgH.List)
			r.Post("/orgs", orgH.Create)
			r.Get("/orgs/{orgID}", orgH.Get)
			r.Put("/orgs/{orgID}", orgH.Update)
			r.Delete("/orgs/{orgID}", orgH.Delete)
			r.Get("/orgs/{orgID}/members", orgH.ListMembers)
			r.Post("/orgs/{orgID}/members", orgH.AddMember)
			r.Delete("/orgs/{orgID}/members/{userID}", orgH.RemoveMember)

			r.Get("/orgs/{orgID}/projects", projectH.List)
			r.Post("/orgs/{orgID}/projects", projectH.Create)
			r.Get("/projects/{projectID}", projectH.Get)
			r.Put("/projects/{projectID}", projectH.Update)
			r.Delete("/projects/{projectID}", projectH.Delete)
			r.Post("/projects/{projectID}/rotate-key", projectH.RotateKey)

			r.Get("/projects/{projectID}/retry-policies", projectH.ListRetryPolicies)
			r.Post("/projects/{projectID}/retry-policies", projectH.CreateRetryPolicy)

			r.Get("/projects/{projectID}/queues", queueH.List)
			r.Post("/projects/{projectID}/queues", queueH.Create)
			r.Get("/queues/{queueID}", queueH.Get)
			r.Put("/queues/{queueID}", queueH.Update)
			r.Delete("/queues/{queueID}", queueH.Delete)
			r.Post("/queues/{queueID}/pause", queueH.Pause)
			r.Post("/queues/{queueID}/resume", queueH.Resume)
			r.Get("/queues/{queueID}/stats", queueH.Stats)

			r.Get("/queues/{queueID}/jobs", jobH.List)
			r.Post("/queues/{queueID}/jobs", jobH.Create)
			r.Post("/queues/{queueID}/jobs/batch", jobH.CreateBatch)
			r.Get("/jobs/{jobID}", jobH.Get)
			r.Delete("/jobs/{jobID}", jobH.Cancel)
			r.Post("/jobs/{jobID}/retry", jobH.Retry)
			r.Delete("/jobs/{jobID}/purge", jobH.Purge)
			r.Get("/jobs/{jobID}/logs", jobH.Logs)
			r.Get("/jobs/{jobID}/executions", jobH.Executions)

			r.Get("/projects/{projectID}/workers", workerH.List)
			r.Get("/workers/{workerID}", workerH.Get)

			r.Get("/queues/{queueID}/dlq", dlqH.List)
			r.Post("/projects/{projectID}/dlq/retry-all", dlqH.RetryAll)
			r.Delete("/projects/{projectID}/dlq/unhandled", dlqH.DiscardUnhandled)
			r.Post("/dlq/{dlqID}/retry", dlqH.Retry)
			r.Delete("/dlq/{dlqID}", dlqH.Discard)

			r.Get("/projects/{projectID}/metrics", metricsH.ProjectMetrics)
			r.Get("/queues/{queueID}/metrics", metricsH.QueueMetrics)
		})
	})

	return r
}
