package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tushar/dis-job-queue/shared/events"
	"github.com/tushar/dis-job-queue/worker/internal/config"
	workerdb "github.com/tushar/dis-job-queue/worker/internal/db"
	"github.com/tushar/dis-job-queue/worker/internal/demo"
	"github.com/tushar/dis-job-queue/worker/internal/executor"
	"github.com/tushar/dis-job-queue/worker/internal/heartbeat"
	"github.com/tushar/dis-job-queue/worker/internal/poller"
)

func runHealthServer(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "10000"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		checkCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		checks := map[string]string{"database": "ok", "redis": "ok"}
		status := http.StatusOK

		if err := pool.Ping(checkCtx); err != nil {
			checks["database"] = "unreachable"
			status = http.StatusServiceUnavailable
		}
		if err := rdb.Ping(checkCtx).Err(); err != nil {
			checks["redis"] = "degraded"
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{
			"ok":     status == http.StatusOK,
			"checks": checks,
		})
	})

	srv := &http.Server{Addr: ":" + port, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Info().Str("port", port).Msg("worker health server listening")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error().Err(err).Msg("health server failed")
	}
}

func simulateWork(name string) executor.Handler {
	return func(ctx context.Context, job *executor.Job) error {
		delay := 10*time.Second + time.Duration(rand.Intn(5000))*time.Millisecond
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
		log.Debug().Str("type", name).Str("job_id", job.ID).
			Dur("simulated_ms", delay).Msg("job handler completed")
		return nil
	}
}

func simulateFail(name string) executor.Handler {
	return func(ctx context.Context, job *executor.Job) error {
		return fmt.Errorf("simulated failure in handler %q", name)
	}
}

var jobTypes = []string{
	"process_order", "sync_inventory", "generate_report", "cleanup_temp_files",
	"send_email", "send_bulk_email", "push_notification", "send_sms",
	"etl_batch", "transcode_video", "process_payment", "fraud_check",
	"compliance_alert", "compliance_report", "heartbeat_check", "delayed_task",
	"batch_op", "batch_process", "idem_job", "cancel_target", "prio_test", "scheduled_cleanup",
	"extract", "transform", "load", "notify", "workflow_step",
}

func main() {
	_ = godotenv.Load()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if os.Getenv("ENV") == "development" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	cfg := config.Load()

	pool, err := workerdb.NewPool(cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer pool.Close()

	rdb := workerdb.NewRedis(cfg.RedisURL)
	defer rdb.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workerID, err := workerdb.RegisterWorker(ctx, pool, cfg.ProjectID, cfg.Concurrency, cfg.QueueNames)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to register worker")
	}
	log.Info().Str("worker_id", workerID).Msg("worker registered")

	bus := events.NewPublisher(rdb, func(err error) {
		log.Debug().Err(err).Msg("event publish failed")
	})

	exec := executor.New(pool, rdb, bus, workerID, cfg)
	for _, jt := range jobTypes {
		exec.Register(jt, simulateWork(jt))
	}
	exec.Register("always_fail", simulateFail("always_fail"))

	if err := workerdb.SetHandledTypes(ctx, pool, workerID, exec.RegisteredTypes()); err != nil {
		log.Warn().Err(err).Msg("failed to publish handled job types")
	}

	poll := poller.New(pool, rdb, exec, bus, workerID, cfg)
	hb := heartbeat.New(pool, rdb, bus, workerID, cfg)

	bus.Publish(ctx, events.Event{
		Type: events.WorkerOnline, ProjectID: cfg.ProjectID, WorkerID: workerID,
	})

	go hb.Run(ctx)
	go poll.RunScheduler(ctx)
	go poll.Run(ctx)
	go runHealthServer(ctx, pool, rdb)

	if cfg.DemoMode {
		go demo.New(pool, rdb, bus, cfg, exec.RegisteredTypes()).Run(ctx)
		log.Info().
			Dur("interval", cfg.DemoInterval).
			Int("burst", cfg.DemoBurst).
			Int("backlog_max", cfg.DemoBacklogMax).
			Msg("demo traffic generator enabled")
	}

	log.Info().
		Str("worker_id", workerID).
		Int("concurrency", cfg.Concurrency).
		Strs("queues", cfg.QueueNames).
		Dur("poll_interval", cfg.PollInterval).
		Msg("worker started")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("graceful shutdown initiated...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	workerdb.MarkWorkerDraining(shutdownCtx, pool, workerID)
	exec.Drain(shutdownCtx)

	poll.Deregister(shutdownCtx)
	workerdb.MarkWorkerOffline(context.Background(), pool, workerID)
	bus.Publish(context.Background(), events.Event{
		Type: events.WorkerOffline, ProjectID: cfg.ProjectID, WorkerID: workerID,
	})
	log.Info().Msg("worker stopped")
}
