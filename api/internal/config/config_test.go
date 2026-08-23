package config

import (
	"testing"
	"time"
)

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pw@localhost:5432/db")
	t.Setenv("JWT_SECRET", "test-secret")
}

func TestLoadUsesDefaultsForOptionalVars(t *testing.T) {
	setRequired(t)
	t.Setenv("PORT", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("JWT_EXPIRY", "")
	t.Setenv("REFRESH_EXPIRY", "")
	t.Setenv("ENV", "")

	cfg := Load()

	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.RedisURL != "redis://localhost:6379" {
		t.Errorf("RedisURL = %q, want the localhost default", cfg.RedisURL)
	}
	if cfg.JWTExpiry != 15*time.Minute {
		t.Errorf("JWTExpiry = %v, want 15m", cfg.JWTExpiry)
	}
	if cfg.RefreshExpiry != 168*time.Hour {
		t.Errorf("RefreshExpiry = %v, want 168h", cfg.RefreshExpiry)
	}
	if cfg.Env != "production" {
		t.Errorf("Env = %q, want %q", cfg.Env, "production")
	}
}

func TestLoadPrefersEnvOverDefaults(t *testing.T) {
	setRequired(t)
	t.Setenv("PORT", "9999")
	t.Setenv("REDIS_URL", "rediss://example.test:6379")
	t.Setenv("JWT_EXPIRY", "30m")
	t.Setenv("REFRESH_EXPIRY", "24h")
	t.Setenv("ENV", "development")

	cfg := Load()

	if cfg.Port != "9999" {
		t.Errorf("Port = %q, want %q", cfg.Port, "9999")
	}
	if cfg.RedisURL != "rediss://example.test:6379" {
		t.Errorf("RedisURL = %q", cfg.RedisURL)
	}
	if cfg.JWTExpiry != 30*time.Minute {
		t.Errorf("JWTExpiry = %v, want 30m", cfg.JWTExpiry)
	}
	if cfg.RefreshExpiry != 24*time.Hour {
		t.Errorf("RefreshExpiry = %v, want 24h", cfg.RefreshExpiry)
	}
	if cfg.Env != "development" {
		t.Errorf("Env = %q, want %q", cfg.Env, "development")
	}
	if cfg.DatabaseURL != "postgres://user:pw@localhost:5432/db" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
}

func TestGetEnvTreatsEmptyAsUnset(t *testing.T) {
	t.Setenv("SOME_OPTIONAL_KEY", "")
	if got := getEnv("SOME_OPTIONAL_KEY", "fallback"); got != "fallback" {
		t.Errorf("getEnv with empty value = %q, want %q", got, "fallback")
	}
	t.Setenv("SOME_OPTIONAL_KEY", "actual")
	if got := getEnv("SOME_OPTIONAL_KEY", "fallback"); got != "actual" {
		t.Errorf("getEnv = %q, want %q", got, "actual")
	}
}

func TestLoadPanicsOnMissingRequiredVars(t *testing.T) {
	for _, missing := range []string{"DATABASE_URL", "JWT_SECRET"} {
		t.Run(missing, func(t *testing.T) {
			setRequired(t)
			t.Setenv(missing, "")

			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("Load() did not panic with %s unset", missing)
				}
				want := "required env var not set: " + missing
				if msg, ok := r.(string); !ok || msg != want {
					t.Errorf("panic = %v, want %q", r, want)
				}
			}()
			Load()
		})
	}
}

func TestParseDurationPanicsOnGarbage(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("parseDuration did not panic on invalid input")
		}
	}()
	parseDuration("not-a-duration")
}
