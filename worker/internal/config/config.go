package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL    string
	RedisURL       string
	ProjectID      string
	QueueNames     []string
	Concurrency    int
	PollInterval   time.Duration
	HeartbeatInterval time.Duration
	Env            string
}

func Load() *Config {
	concurrency := 5
	if v := os.Getenv("WORKER_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			concurrency = n
		}
	}

	queues := strings.Split(getEnv("WORKER_QUEUES", "default"), ",")
	for i, q := range queues {
		queues[i] = strings.TrimSpace(q)
	}

	return &Config{
		DatabaseURL:       mustEnv("DATABASE_URL"),
		RedisURL:          getEnv("REDIS_URL", "redis://localhost:6379"),
		ProjectID:         mustEnv("PROJECT_ID"),
		QueueNames:        queues,
		Concurrency:       concurrency,
		PollInterval:      parseDuration(getEnv("POLL_INTERVAL", "500ms")),
		HeartbeatInterval: parseDuration(getEnv("HEARTBEAT_INTERVAL", "10s")),
		Env:               getEnv("ENV", "production"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic("required env var not set: " + key)
	}
	return v
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		panic("invalid duration: " + s)
	}
	return d
}
