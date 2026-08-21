package config

import (
	"os"
	"time"
)

type Config struct {
	Port           string
	DatabaseURL    string
	RedisURL       string
	JWTSecret      string
	JWTExpiry      time.Duration
	RefreshExpiry  time.Duration
	Env            string
}

func Load() *Config {
	return &Config{
		Port:          getEnv("PORT", "8080"),
		DatabaseURL:   mustEnv("DATABASE_URL"),
		RedisURL:      getEnv("REDIS_URL", "redis://localhost:6379"),
		JWTSecret:     mustEnv("JWT_SECRET"),
		JWTExpiry:     parseDuration(getEnv("JWT_EXPIRY", "15m")),
		RefreshExpiry: parseDuration(getEnv("REFRESH_EXPIRY", "168h")),
		Env:           getEnv("ENV", "production"),
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
