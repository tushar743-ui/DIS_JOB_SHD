package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL       string
	RedisURL          string
	ProjectID         string
	QueueNames        []string
	Concurrency       int
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	Env               string
	DemoMode          bool
	DemoInterval      time.Duration
	DemoBurst         int
	DemoBacklogMax    int
	DemoRetention     time.Duration
}

func Load() *Config {
	concurrency := 5
	if v := os.Getenv("WORKER_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			concurrency = n
		}
	}

	var queues []string
	if v := os.Getenv("WORKER_QUEUES"); v != "" {
		queues = strings.Split(v, ",")
		for i, q := range queues {
			queues[i] = strings.TrimSpace(q)
		}
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
		DemoMode:          strings.EqualFold(getEnv("DEMO_MODE", "false"), "true"),
		DemoInterval:      parseDuration(getEnv("DEMO_INTERVAL", "3s")),
		DemoBurst:         parsePositiveInt(getEnv("DEMO_BURST", "2"), 2),
		DemoBacklogMax:    parsePositiveInt(getEnv("DEMO_BACKLOG_MAX", "60"), 60),
		DemoRetention:     parseDuration(getEnv("DEMO_RETENTION", "2h")),
	}
}

func parsePositiveInt(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
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
