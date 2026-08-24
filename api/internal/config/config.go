package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port             string
	DatabaseURL      string
	RedisURL         string
	JWTSecret        string
	JWTExpiry        time.Duration
	RefreshExpiry    time.Duration
	Env              string
	CORSOrigins      []string
	RateLimit        int
	RateLimitWindow  time.Duration
	AnthropicAPIKey  string
	AISummaryModel   string
}

func Load() *Config {
	return &Config{
		Port:            getEnv("PORT", "8080"),
		DatabaseURL:     mustEnv("DATABASE_URL"),
		RedisURL:        getEnv("REDIS_URL", "redis://localhost:6379"),
		JWTSecret:       mustEnv("JWT_SECRET"),
		JWTExpiry:       parseDuration(getEnv("JWT_EXPIRY", "15m")),
		RefreshExpiry:   parseDuration(getEnv("REFRESH_EXPIRY", "168h")),
		Env:             getEnv("ENV", "production"),
		CORSOrigins:     parseList(getEnv("CORS_ORIGINS", "*")),
		RateLimit:       parseInt(getEnv("RATE_LIMIT", "200"), 200),
		RateLimitWindow: parseDuration(getEnv("RATE_LIMIT_WINDOW", "1m")),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		AISummaryModel:  getEnv("AI_SUMMARY_MODEL", "claude-opus-5"),
	}
}

func (c *Config) AISummariesEnabled() bool {
	return strings.TrimSpace(c.AnthropicAPIKey) != ""
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

func parseInt(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func parseList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
