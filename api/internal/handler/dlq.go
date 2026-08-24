package handler

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tushar/dis-job-queue/api/internal/middleware"
	"github.com/tushar/dis-job-queue/shared/events"
)

type DLQHandler struct {
	db  *pgxpool.Pool
	bus *events.Publisher
}

func NewDLQHandler(db *pgxpool.Pool, bus *events.Publisher) *DLQHandler {
	return &DLQHandler{db: db, bus: bus}
}

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

	var jobID, jobType, projectID, queueID string
	if err := h.db.QueryRow(r.Context(),
		`SELECT d.job_id, j.type, q.project_id, q.id
		 FROM dead_letter_queue d
		 JOIN jobs j ON j.id = d.job_id
		 JOIN queues q ON q.id = d.queue_id
		 WHERE d.id=$1 AND d.resolved_at IS NULL`, dlqID,
	).Scan(&jobID, &jobType, &projectID, &queueID); err != nil {
		writeError(w, http.StatusNotFound, "DLQ entry not found or already resolved")
		return
	}

	handled, err := handledTypes(r.Context(), h.db, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read worker capabilities: "+err.Error())
		return
	}
	if !slices.Contains(handled, jobType) {
		writeError(w, http.StatusConflict,
			"no live worker handles job type \""+jobType+"\" - retrying it would fail again")
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback(r.Context())

	if _, err := tx.Exec(r.Context(),
		`UPDATE jobs SET status='queued', attempt_count=0, last_error=NULL, completed_at=NULL,
		   claimed_by=NULL, claimed_at=NULL, run_at=now(), updated_at=now()
		 WHERE id=$1`, jobID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to requeue job")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE dead_letter_queue SET resolved_at=now(), resolved_by=$1 WHERE id=$2`, userID, dlqID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve dead-letter entry")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	h.bus.Publish(r.Context(), events.Event{
		Type: events.JobEnqueued, ProjectID: projectID,
		QueueID: queueID, JobID: jobID, JobType: jobType, Status: "queued",
	})
	writeJSON(w, http.StatusOK, map[string]string{"message": "job re-queued", "job_id": jobID})
}

const liveWorkerWindow = "2 minutes"

func handledTypes(ctx context.Context, db *pgxpool.Pool, projectID string) ([]string, error) {
	var types []string
	err := db.QueryRow(ctx,
		`SELECT COALESCE(array_agg(DISTINCT t), '{}')
		 FROM workers w, unnest(w.handled_types) AS t
		 WHERE w.project_id=$1 AND w.status='active'
		   AND w.last_heartbeat_at > now() - interval '`+liveWorkerWindow+`'`,
		projectID).Scan(&types)
	return types, err
}

func (h *DLQHandler) RetryAll(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	userID := middleware.UserIDFromContext(r.Context())

	handled, err := handledTypes(r.Context(), h.db, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read worker capabilities: "+err.Error())
		return
	}
	if len(handled) == 0 {
		writeError(w, http.StatusConflict,
			"no live worker in this project is advertising job handlers - start a worker before retrying")
		return
	}

	var requeued, skipped int
	err = h.db.QueryRow(r.Context(),
		`WITH pending AS (
		     SELECT d.id, d.job_id, j.type
		     FROM dead_letter_queue d
		     JOIN jobs j ON j.id = d.job_id
		     WHERE d.resolved_at IS NULL
		       AND d.queue_id IN (SELECT id FROM queues WHERE project_id=$2)
		 ), resolved AS (
		     UPDATE dead_letter_queue SET resolved_at=now(), resolved_by=$1
		     WHERE id IN (SELECT id FROM pending WHERE type = ANY($3))
		     RETURNING job_id
		 ), requeued AS (
		     UPDATE jobs SET status='queued', attempt_count=0, last_error=NULL,
		       run_at=now(), updated_at=now(), completed_at=NULL, claimed_by=NULL
		     WHERE id IN (SELECT job_id FROM resolved)
		     RETURNING id
		 )
		 SELECT (SELECT COUNT(*) FROM requeued),
		        (SELECT COUNT(*) FROM pending WHERE NOT (type = ANY($3)))`,
		userID, projectID, handled,
	).Scan(&requeued, &skipped)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retry dead-letter jobs: "+err.Error())
		return
	}

	if requeued > 0 {
		h.bus.Publish(r.Context(), events.Event{
			Type: events.JobEnqueued, ProjectID: projectID, Status: "queued",
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"requeued":          requeued,
		"skipped_unhandled": skipped,
	})
}

func (h *DLQHandler) DiscardUnhandled(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	userID := middleware.UserIDFromContext(r.Context())

	handled, err := handledTypes(r.Context(), h.db, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read worker capabilities: "+err.Error())
		return
	}
	if len(handled) == 0 {
		writeError(w, http.StatusConflict,
			"no live worker in this project is advertising job handlers - cannot tell which entries are unrunnable")
		return
	}

	var discarded int
	err = h.db.QueryRow(r.Context(),
		`WITH unhandled AS (
		     SELECT d.id
		     FROM dead_letter_queue d
		     JOIN jobs j ON j.id = d.job_id
		     WHERE d.resolved_at IS NULL
		       AND d.queue_id IN (SELECT id FROM queues WHERE project_id=$2)
		       AND NOT (j.type = ANY($3))
		 ), discarded AS (
		     UPDATE dead_letter_queue SET resolved_at=now(), resolved_by=$1
		     WHERE id IN (SELECT id FROM unhandled)
		     RETURNING id
		 )
		 SELECT COUNT(*) FROM discarded`, userID, projectID, handled,
	).Scan(&discarded)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to discard dead-letter jobs: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"discarded": discarded})
}

func (h *DLQHandler) Discard(w http.ResponseWriter, r *http.Request) {
	dlqID := chi.URLParam(r, "dlqID")
	userID := middleware.UserIDFromContext(r.Context())
	tag, err := h.db.Exec(r.Context(),
		`UPDATE dead_letter_queue SET resolved_at=now(), resolved_by=$1 WHERE id=$2`, userID, dlqID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "DLQ entry not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
