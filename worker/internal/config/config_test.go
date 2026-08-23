package config

import (
	"testing"
	"time"
)

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pw@localhost:5432/db")
	t.Setenv("PROJECT_ID", "11111111-2222-3333-4444-555555555555")
}

func clearOptional(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"REDIS_URL", "WORKER_QUEUES", "WORKER_CONCURRENCY",
		"POLL_INTERVAL", "HEARTBEAT_INTERVAL", "ENV",
	} {
		t.Setenv(k, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	setRequired(t)
	clearOptional(t)

	cfg := Load()

	if cfg.RedisURL != "redis://localhost:6379" {
		t.Errorf("RedisURL = %q", cfg.RedisURL)
	}
	if len(cfg.QueueNames) != 1 || cfg.QueueNames[0] != "default" {
		t.Errorf("QueueNames = %#v, want [default]", cfg.QueueNames)
	}
	if cfg.Concurrency != 5 {
		t.Errorf("Concurrency = %d, want 5", cfg.Concurrency)
	}
	if cfg.PollInterval != 500*time.Millisecond {
		t.Errorf("PollInterval = %v, want 500ms", cfg.PollInterval)
	}
	if cfg.HeartbeatInterval != 10*time.Second {
		t.Errorf("HeartbeatInterval = %v, want 10s", cfg.HeartbeatInterval)
	}
	if cfg.Env != "production" {
		t.Errorf("Env = %q, want production", cfg.Env)
	}
}

func TestLoadParsesQueueNames(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want []string
	}{
		{"single", "email", []string{"email"}},
		{"multiple", "default,email,notifications", []string{"default", "email", "notifications"}},
		{"spaces are trimmed", " default , email ", []string{"default", "email"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			clearOptional(t)
			t.Setenv("WORKER_QUEUES", tc.env)

			got := Load().QueueNames
			if len(got) != len(tc.want) {
				t.Fatalf("QueueNames = %#v, want %#v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("QueueNames[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestLoadConcurrency(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int
	}{
		{"valid value is used", "12", 12},
		{"unparseable value falls back to the default", "many", 5},
		{"empty value falls back to the default", "", 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			clearOptional(t)
			t.Setenv("WORKER_CONCURRENCY", tc.env)

			if got := Load().Concurrency; got != tc.want {
				t.Errorf("Concurrency = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestLoadPanicsOnMissingRequiredVars(t *testing.T) {
	for _, missing := range []string{"DATABASE_URL", "PROJECT_ID"} {
		t.Run(missing, func(t *testing.T) {
			setRequired(t)
			clearOptional(t)
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
	parseDuration("10 seconds")
}
