package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tushar/dis-job-queue/worker/internal/config"
	workerdb "github.com/tushar/dis-job-queue/worker/internal/db"
	"github.com/tushar/dis-job-queue/worker/internal/executor"
	"github.com/tushar/dis-job-queue/worker/internal/heartbeat"
	"github.com/tushar/dis-job-queue/worker/internal/poller"
)

func main() {
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
