package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tushar/dis-job-queue/worker/internal/config"
	workerdb "github.com/tushar/dis-job-queue/worker/internal/db"
	"github.com/tushar/dis-job-queue/worker/internal/executor"
	"github.com/tushar/dis-job-queue/worker/internal/heartbeat"
	"github.com/tushar/dis-job-queue/worker/internal/poller"
)

// simulateWork returns a handler that sleeps for a random short duration and succeeds.
// jobTypes listed here represent real application work — the handler simulates execution.
func simulateWork(name string) executor.Handler {
	return func(ctx context.Context, job *executor.Job) error {
		delay := time.Duration(50+rand.Intn(200)) * time.Millisecond
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

// simulateFail returns a handler that always returns an error (to test retry/DLQ).
func simulateFail(name string) executor.Handler {
	return func(ctx context.Context, job *executor.Job) error {
		return fmt.Errorf("simulated failure in handler %q", name)
	}
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

	exec := executor.New(pool, rdb, workerID, cfg)

	// Register handlers for all known job types.
	// These simulate application-level work — in production these would call real services.
	jobTypes := []string{
		"process_order", "sync_inventory", "generate_report", "cleanup_temp_files",
		"send_email", "send_bulk_email", "push_notification", "send_sms",
		"etl_batch", "transcode_video", "process_payment", "fraud_check",
		"compliance_alert", "compliance_report", "heartbeat_check", "delayed_task",
		"batch_op", "batch_process", "idem_job", "cancel_target", "prio_test", "scheduled_cleanup",
	}
	for _, jt := range jobTypes {
		exec.Register(jt, simulateWork(jt))
	}
	// Intentionally failing handlers — used to exercise retry and DLQ paths.
	exec.Register("always_fail", simulateFail("always_fail"))

	poll := poller.New(pool, rdb, exec, workerID, cfg)
	hb := heartbeat.New(pool, workerID)

	go hb.Run(ctx)
	go poll.RunScheduler(ctx)
	go poll.Run(ctx)

	log.Info().
		Str("worker_id", workerID).
		Int("concurrency", cfg.Concurrency).
		Strs("queues", cfg.QueueNames).
		Msg("worker started")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("graceful shutdown initiated...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	exec.Drain(shutdownCtx)

	workerdb.MarkWorkerOffline(context.Background(), pool, workerID)
	log.Info().Msg("worker stopped")
}
