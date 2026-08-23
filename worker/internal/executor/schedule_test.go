package executor

import (
	"testing"
	"time"
)

// ─── NextCronRun unit tests (no DB) ──────────────────────────────────────────

func TestNextCronRun_SixFieldWithSeconds(t *testing.T) {
	from := time.Date(2026, 3, 1, 10, 0, 3, 0, time.UTC)
	next, err := NextCronRun("*/10 * * * * *", from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 3, 1, 10, 0, 10, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next run = %v, want %v", next, want)
	}
}

func TestNextCronRun_StandardFiveField(t *testing.T) {
	from := time.Date(2026, 3, 1, 10, 2, 0, 0, time.UTC)
	next, err := NextCronRun("*/5 * * * *", from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 3, 1, 10, 5, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next run = %v, want %v", next, want)
	}
}

func TestNextCronRun_Descriptor(t *testing.T) {
	from := time.Date(2026, 3, 1, 10, 30, 0, 0, time.UTC)
	next, err := NextCronRun("@hourly", from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next run = %v, want %v", next, want)
	}
}

func TestNextCronRun_AlwaysInTheFuture(t *testing.T) {
	from := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	for _, expr := range []string{"*/10 * * * * *", "0 * * * *", "@daily", "0 0 2 * * *"} {
		next, err := NextCronRun(expr, from)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", expr, err)
		}
		if !next.After(from) {
			t.Fatalf("%s: next run %v is not after %v", expr, next, from)
		}
	}
}

func TestNextCronRun_InvalidExpression(t *testing.T) {
	for _, expr := range []string{"", "not a cron", "* * *", "99 * * * *"} {
		if _, err := NextCronRun(expr, time.Now()); err == nil {
			t.Fatalf("expected an error for %q", expr)
		}
	}
}
