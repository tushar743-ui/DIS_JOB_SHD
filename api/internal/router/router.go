package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/tushar/dis-job-queue/api/internal/ai"
	"github.com/tushar/dis-job-queue/api/internal/authz"
	"github.com/tushar/dis-job-queue/api/internal/config"
	"github.com/tushar/dis-job-queue/api/internal/handler"
	"github.com/tushar/dis-job-queue/api/internal/middleware"
	"github.com/tushar/dis-job-queue/shared/events"
)

type Deps struct {
	Config *config.Config
	Pool   *pgxpool.Pool
	Redis  *redis.Client
	Hub    *handler.Hub
	Bus    *events.Publisher
}

func New(d Deps) http.Handler {
	cfg := d.Config
	r := chi.NewRouter()

	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.CORS(cfg.CORSOrigins))

	summarizer := ai.New(cfg.GroqAPIKey, cfg.AISummaryModel)
	guard := authz.New(d.Pool)

	viewer := guard.Require(authz.RoleViewer)
	member := guard.Require(authz.RoleMember)
	admin := guard.Require(authz.RoleAdmin)
	owner := guard.Require(authz.RoleOwner)

	authH := handler.NewAuthHandler(d.Pool, cfg)
	orgH := handler.NewOrgHandler(d.Pool)
	projectH := handler.NewProjectHandler(d.Pool)
	queueH := handler.NewQueueHandler(d.Pool, d.Bus)
	jobH := handler.NewJobHandler(d.Pool, d.Bus)
	workerH := handler.NewWorkerHandler(d.Pool)
	dlqH := handler.NewDLQHandler(d.Pool, d.Bus)
	metricsH := handler.NewMetricsHandler(d.Pool)
	summaryH := handler.NewFailureSummaryHandler(d.Pool, d.Redis, summarizer)
	liveH := handler.NewLiveHandler(d.Hub)

	r.Get("/health", handler.Health(d.Pool, d.Redis))

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.RateLimiter(d.Redis, cfg.RateLimit, cfg.RateLimitWindow))

		r.Post("/auth/register", authH.Register)
		r.Post("/auth/login", authH.Login)
		r.Post("/auth/demo", authH.DemoLogin)
		r.Post("/auth/refresh", authH.Refresh)
		r.Get("/features", handler.Features(cfg))

		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWTSecret))

			r.Post("/auth/logout", authH.Logout)
			r.Get("/auth/me", authH.Me)
			r.Get("/orgs", orgH.List)
			r.Post("/orgs", orgH.Create)

			r.With(viewer).Get("/orgs/{orgID}", orgH.Get)
			r.With(admin).Put("/orgs/{orgID}", orgH.Update)
			r.With(owner).Delete("/orgs/{orgID}", orgH.Delete)
			r.With(viewer).Get("/orgs/{orgID}/members", orgH.ListMembers)
			r.With(admin).Post("/orgs/{orgID}/members", orgH.AddMember)
			r.With(admin).Delete("/orgs/{orgID}/members/{userID}", orgH.RemoveMember)

			r.With(viewer).Get("/orgs/{orgID}/projects", projectH.List)
			r.With(admin).Post("/orgs/{orgID}/projects", projectH.Create)
			r.With(viewer).Get("/projects/{projectID}", projectH.Get)
			r.With(admin).Put("/projects/{projectID}", projectH.Update)
			r.With(owner).Delete("/projects/{projectID}", projectH.Delete)
			r.With(owner).Post("/projects/{projectID}/rotate-key", projectH.RotateKey)

			r.With(viewer).Get("/projects/{projectID}/retry-policies", projectH.ListRetryPolicies)
			r.With(admin).Post("/projects/{projectID}/retry-policies", projectH.CreateRetryPolicy)

			r.With(viewer).Get("/projects/{projectID}/queues", queueH.List)
			r.With(admin).Post("/projects/{projectID}/queues", queueH.Create)
			r.With(viewer).Get("/queues/{queueID}", queueH.Get)
			r.With(admin).Put("/queues/{queueID}", queueH.Update)
			r.With(admin).Delete("/queues/{queueID}", queueH.Delete)
			r.With(member).Post("/queues/{queueID}/pause", queueH.Pause)
			r.With(member).Post("/queues/{queueID}/resume", queueH.Resume)
			r.With(viewer).Get("/queues/{queueID}/stats", queueH.Stats)

			r.With(viewer).Get("/projects/{projectID}/jobs", jobH.ListByProject)
			r.With(viewer).Get("/projects/{projectID}/job-types", jobH.HandledTypes)
			r.With(viewer).Get("/queues/{queueID}/jobs", jobH.List)
			r.With(member).Post("/queues/{queueID}/jobs", jobH.Create)
			r.With(member).Post("/queues/{queueID}/jobs/batch", jobH.CreateBatch)
			r.With(viewer).Get("/jobs/{jobID}", jobH.Get)
			r.With(viewer).Get("/jobs/{jobID}/dependencies", jobH.Dependencies)
			r.With(member).Delete("/jobs/{jobID}", jobH.Cancel)
			r.With(member).Post("/jobs/{jobID}/retry", jobH.Retry)
			r.With(admin).Delete("/jobs/{jobID}/purge", jobH.Purge)
			r.With(viewer).Get("/jobs/{jobID}/logs", jobH.Logs)
			r.With(viewer).Get("/jobs/{jobID}/executions", jobH.Executions)

			r.With(viewer).Get("/jobs/{jobID}/failure-summary", summaryH.Get)
			r.With(member).Post("/jobs/{jobID}/failure-summary", summaryH.Generate)

			r.With(viewer).Get("/projects/{projectID}/workers", workerH.List)
			r.With(viewer).Get("/workers/{workerID}", workerH.Get)

			r.With(viewer).Get("/queues/{queueID}/dlq", dlqH.List)
			r.With(member).Post("/projects/{projectID}/dlq/retry-all", dlqH.RetryAll)
			r.With(admin).Delete("/projects/{projectID}/dlq/unhandled", dlqH.DiscardUnhandled)
			r.With(member).Post("/dlq/{dlqID}/retry", dlqH.Retry)
			r.With(admin).Delete("/dlq/{dlqID}", dlqH.Discard)

			r.With(viewer).Get("/projects/{projectID}/metrics", metricsH.ProjectMetrics)
			r.With(viewer).Get("/queues/{queueID}/metrics", metricsH.QueueMetrics)

			r.With(viewer).Get("/projects/{projectID}/events", liveH.Stream)
		})
	})

	return r
}
