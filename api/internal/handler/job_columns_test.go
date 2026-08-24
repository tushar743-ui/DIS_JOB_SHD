package handler

import (
	"strings"
	"testing"
)

func TestPrefixed(t *testing.T) {
	tests := []struct {
		name    string
		columns string
		alias   string
		want    string
	}{
		{"single", "id", "j", "j.id"},
		{"multiple", "id, type, priority", "j", "j.id, j.type, j.priority"},
		{"cast is preserved after the alias", "status::text", "j", "j.status::text"},
		{"mixed with cast", "id, status::text, priority", "j", "j.id, j.status::text, j.priority"},
		{"newlines and tabs are normalized", "id,\n\ttype", "j", "j.id, j.type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prefixed(tt.columns, tt.alias); got != tt.want {
				t.Errorf("prefixed(%q, %q) = %q, want %q", tt.columns, tt.alias, got, tt.want)
			}
		})
	}
}

func TestPrefixedCoversEveryJobColumn(t *testing.T) {
	got := prefixed(jobColumns, "j")

	wantCount := strings.Count(jobColumns, ",") + 1
	if gotCount := strings.Count(got, ","); gotCount+1 != wantCount {
		t.Fatalf("column count changed: got %d, want %d", gotCount+1, wantCount)
	}

	for _, col := range strings.Split(got, ", ") {
		if !strings.HasPrefix(col, "j.") {
			t.Errorf("column %q is not alias-qualified", col)
		}
		if strings.Count(col, "j.") != 1 {
			t.Errorf("column %q was qualified more than once", col)
		}
	}
}

func TestPrefixedScanOrderMatchesSelectOrder(t *testing.T) {
	if strings.Count(jobColumns, ",")+1 != 21 {
		t.Fatalf("jobColumns changed shape; scanJobWithQueue must be updated to match")
	}
}
