package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tushar/dis-job-queue/api/internal/config"
	"github.com/tushar/dis-job-queue/api/internal/db"
	"github.com/tushar/dis-job-queue/api/internal/handler"
	"github.com/tushar/dis-job-queue/api/internal/router"
	"github.com/tushar/dis-job-queue/shared/events"
)

func main() {
	_ = godotenv.Load()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if os.Getenv("ENV") == "development" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	cfg := config.Load()

	pool, err := db.NewPool(cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer pool.Close()

	rdb := db.NewRedis(cfg.RedisURL)
	defer rdb.Close()

	hub := handler.NewHub(rdb)
	defer hub.Close()

	bus := events.NewPublisher(rdb, func(err error) {
		log.Debug().Err(err).Msg("event publish failed")
	})

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: router.New(router.Deps{
			Config: cfg, Pool: pool, Redis: rdb, Hub: hub, Bus: bus,
		}),
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Info().
			Str("port", cfg.Port).
			Bool("ai_summaries", cfg.AISummariesEnabled()).
			Msg("API server starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down server...")
	hub.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("server forced shutdown")
	}
	log.Info().Msg("server stopped")
}
