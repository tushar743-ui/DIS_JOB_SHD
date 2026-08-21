package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tushar/dis-job-queue/api/internal/middleware"
)

type DLQHandler struct{ db *pgxpool.Pool }

func NewDLQHandler(db *pgxpool.Pool) *DLQHandler { return &DLQHandler{db: db} }

type dlqRow struct {
	ID         string     `json:"id"`
	JobID      string     `json:"job_id"`
	QueueID    string     `json:"queue_id"`
	FinalError *string    `json:"final_error,omitempty"`
	Attempts   int        `json:"attempts"`
	MovedAt    time.Time  `json:"moved_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

func (h *DLQHandler) List(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	limit, offset := pageParams(r)
	rows, err := h.db.Query(r.Context(),
		`SELECT id, job_id, queue_id, final_error, attempts, moved_at, resolved_at
		 FROM dead_letter_queue WHERE queue_id=$1
		 ORDER BY moved_at DESC LIMIT $2 OFFSET $3`,
		queueID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	entries := []dlqRow{}
	for rows.Next() {
		var d dlqRow
		rows.Scan(&d.ID, &d.JobID, &d.QueueID, &d.FinalError, &d.Attempts, &d.MovedAt, &d.ResolvedAt)
		entries = append(entries, d)
	}

	var total int
	h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM dead_letter_queue WHERE queue_id=$1`, queueID).Scan(&total)
	writeJSON(w, http.StatusOK, paginated[dlqRow]{Data: entries, Total: total, Limit: limit, Offset: offset})
}

func (h *DLQHandler) Retry(w http.ResponseWriter, r *http.Request) {
	dlqID := chi.URLParam(r, "dlqID")
	userID := middleware.UserIDFromContext(r.Context())

	var jobID string
	if err := h.db.QueryRow(r.Context(),
		`SELECT job_id FROM dead_letter_queue WHERE id=$1 AND resolved_at IS NULL`, dlqID,
	).Scan(&jobID); err != nil {
		writeError(w, http.StatusNotFound, "DLQ entry not found or already resolved")
		return
	}

	tx, _ := h.db.Begin(r.Context())
	defer tx.Rollback(r.Context())

	tx.Exec(r.Context(),
		`UPDATE jobs SET status='queued', attempt_count=0, last_error=NULL, run_at=now(), updated_at=now()
		 WHERE id=$1`, jobID)
	tx.Exec(r.Context(),
		`UPDATE dead_letter_queue SET resolved_at=now(), resolved_by=$1 WHERE id=$2`, userID, dlqID)

	tx.Commit(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{"message": "job re-queued", "job_id": jobID})
}

func (h *DLQHandler) Discard(w http.ResponseWriter, r *http.Request) {
	dlqID := chi.URLParam(r, "dlqID")
	userID := middleware.UserIDFromContext(r.Context())
	h.db.Exec(r.Context(),
		`UPDATE dead_letter_queue SET resolved_at=now(), resolved_by=$1 WHERE id=$2`, userID, dlqID)
	w.WriteHeader(http.StatusNoContent)
}
