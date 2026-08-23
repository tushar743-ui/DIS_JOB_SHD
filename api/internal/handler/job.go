package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type JobHandler struct{ db *pgxpool.Pool }

func NewJobHandler(db *pgxpool.Pool) *JobHandler { return &JobHandler{db: db} }

type jobRow struct {
	ID             string          `json:"id"`
	QueueID        string          `json:"queue_id"`
	Type           string          `json:"type"`
	Payload        json.RawMessage `json:"payload"`
	Status         string          `json:"status"`
	Priority       int             `json:"priority"`
	MaxAttempts    int             `json:"max_attempts"`
	AttemptCount   int             `json:"attempt_count"`
	ScheduledAt    *time.Time      `json:"scheduled_at,omitempty"`
	RunAt          time.Time       `json:"run_at"`
	TimeoutSecs    int             `json:"timeout_secs"`
	CronExpression *string         `json:"cron_expression,omitempty"`
	BatchID        *string         `json:"batch_id,omitempty"`
	IdempotencyKey *string         `json:"idempotency_key,omitempty"`
	Tags           []string        `json:"tags"`
	LastError      *string         `json:"last_error,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
}

func (h *JobHandler) List(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	limit, offset := pageParams(r)
	status := r.URL.Query().Get("status")

	var (
		rows interface{ Close() }
		err  error
	)

	const baseCol = `SELECT id, queue_id, type, payload, status::text, priority, max_attempts, attempt_count,
	           scheduled_at, run_at, timeout_secs, cron_expression, batch_id, idempotency_key,
	           tags, last_error, created_at, updated_at, completed_at FROM jobs`

	if status != "" {
		rows, err = h.db.Query(r.Context(),
			baseCol+` WHERE queue_id=$1 AND status=$2::job_status ORDER BY created_at DESC LIMIT $3 OFFSET $4`,
			queueID, status, limit, offset)
	} else {
		rows, err = h.db.Query(r.Context(),
			baseCol+` WHERE queue_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
			queueID, limit, offset)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	pgRows, ok := rows.(interface {
		Next() bool
		Scan(...any) error
		Close()
	})
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer pgRows.Close()

	jobs := []jobRow{}
	for pgRows.Next() {
		var j jobRow
		pgRows.Scan(&j.ID, &j.QueueID, &j.Type, &j.Payload, &j.Status, &j.Priority,
			&j.MaxAttempts, &j.AttemptCount, &j.ScheduledAt, &j.RunAt, &j.TimeoutSecs,
			&j.CronExpression, &j.BatchID, &j.IdempotencyKey, &j.Tags, &j.LastError,
			&j.CreatedAt, &j.UpdatedAt, &j.CompletedAt)
		jobs = append(jobs, j)
	}

	var total int
	if status != "" {
		h.db.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM jobs WHERE queue_id=$1 AND status=$2::job_status`, queueID, status,
		).Scan(&total)
	} else {
		h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM jobs WHERE queue_id=$1`, queueID).Scan(&total)
	}
	writeJSON(w, http.StatusOK, paginated[jobRow]{Data: jobs, Total: total, Limit: limit, Offset: offset})
}

type createJobRequest struct {
	Type           string          `json:"type"`
	Payload        json.RawMessage `json:"payload"`
	Priority       int             `json:"priority"`
	MaxAttempts    int             `json:"max_attempts"`
	ScheduledAt    *time.Time      `json:"scheduled_at"`
	TimeoutSecs    int             `json:"timeout_secs"`
	CronExpression *string         `json:"cron_expression"`
	IdempotencyKey *string         `json:"idempotency_key"`
	Tags           []string        `json:"tags"`
}

func (h *JobHandler) Create(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	var req createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Type == "" {
		writeError(w, http.StatusBadRequest, "type required")
		return
	}

	var paused bool
	if err := h.db.QueryRow(r.Context(),
		`SELECT paused FROM queues WHERE id=$1`, queueID,
	).Scan(&paused); err != nil {
		writeError(w, http.StatusNotFound, "queue not found")
		return
	}

	if req.Priority == 0 {
		req.Priority = 5
	}
	if req.MaxAttempts == 0 {
		req.MaxAttempts = 3
	}
	if req.TimeoutSecs == 0 {
		req.TimeoutSecs = 300
	}
	if req.Payload == nil {
		req.Payload = json.RawMessage("{}")
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}

	runAt := time.Now()
	status := "queued"
	if req.ScheduledAt != nil && req.ScheduledAt.After(time.Now()) {
		runAt = *req.ScheduledAt
		status = "scheduled"
	}

	var jobID string
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO jobs (queue_id, type, payload, status, priority, max_attempts, scheduled_at, run_at,
		  timeout_secs, cron_expression, idempotency_key, tags)
		 VALUES ($1,$2,$3,$4::job_status,$5,$6,$7,$8,$9,$10,$11,$12)
		 ON CONFLICT (idempotency_key) DO NOTHING
		 RETURNING id`,
		queueID, req.Type, []byte(req.Payload), status, req.Priority, req.MaxAttempts,
		req.ScheduledAt, runAt, req.TimeoutSecs, req.CronExpression, req.IdempotencyKey, req.Tags,
	).Scan(&jobID)
	if err != nil {
		// pgx returns pgx.ErrNoRows when ON CONFLICT DO NOTHING suppresses the INSERT
		if err.Error() == "no rows in result set" {
			writeError(w, http.StatusConflict, "duplicate idempotency_key")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create job: "+err.Error())
		return
	}
	if jobID == "" {
		writeError(w, http.StatusConflict, "duplicate idempotency_key")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": jobID, "status": status})
}

func (h *JobHandler) CreateBatch(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	var reqs []createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil || len(reqs) == 0 {
		writeError(w, http.StatusBadRequest, "array of jobs required")
		return
	}
	if len(reqs) > 1000 {
		writeError(w, http.StatusBadRequest, "maximum 1000 jobs per batch")
		return
	}

	batchID := uuid.New().String()
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback(r.Context())

	ids := make([]string, 0, len(reqs))
	for _, req := range reqs {
		if req.Priority == 0 {
			req.Priority = 5
		}
		if req.MaxAttempts == 0 {
			req.MaxAttempts = 3
		}
		if req.TimeoutSecs == 0 {
			req.TimeoutSecs = 300
		}
		if req.Payload == nil {
			req.Payload = json.RawMessage("{}")
		}
		if req.Tags == nil {
			req.Tags = []string{}
		}
		runAt := time.Now()
		batchStatus := "queued"
		if req.ScheduledAt != nil && req.ScheduledAt.After(time.Now()) {
			runAt = *req.ScheduledAt
			batchStatus = "scheduled"
		}
		var id string
		if err := tx.QueryRow(r.Context(),
			`INSERT INTO jobs (queue_id, type, payload, status, priority, max_attempts,
			  scheduled_at, run_at, timeout_secs, batch_id, idempotency_key, tags)
			 VALUES ($1,$2,$3,$4::job_status,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`,
			queueID, req.Type, []byte(req.Payload), batchStatus, req.Priority, req.MaxAttempts,
			req.ScheduledAt, runAt, req.TimeoutSecs, batchID, req.IdempotencyKey, req.Tags,
		).Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"batch_id": batchID,
		"count":    len(ids),
		"job_ids":  ids,
	})
}

func (h *JobHandler) Get(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	var j jobRow
	err := h.db.QueryRow(r.Context(),
		`SELECT id, queue_id, type, payload, status::text, priority, max_attempts, attempt_count,
		  scheduled_at, run_at, timeout_secs, cron_expression, batch_id, idempotency_key,
		  tags, last_error, created_at, updated_at, completed_at
		 FROM jobs WHERE id=$1`, jobID,
	).Scan(&j.ID, &j.QueueID, &j.Type, &j.Payload, &j.Status, &j.Priority,
		&j.MaxAttempts, &j.AttemptCount, &j.ScheduledAt, &j.RunAt, &j.TimeoutSecs,
		&j.CronExpression, &j.BatchID, &j.IdempotencyKey, &j.Tags, &j.LastError,
		&j.CreatedAt, &j.UpdatedAt, &j.CompletedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (h *JobHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	tag, err := h.db.Exec(r.Context(),
		`UPDATE jobs SET status='cancelled', updated_at=now()
		 WHERE id=$1 AND status IN ('queued','scheduled')`, jobID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "job cannot be cancelled")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "cancelled"})
}

func (h *JobHandler) Retry(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	tag, err := h.db.Exec(r.Context(),
		`UPDATE jobs SET status='queued', attempt_count=0, last_error=NULL,
		  run_at=now(), updated_at=now()
		 WHERE id=$1 AND status IN ('failed','dead','cancelled')`, jobID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "job cannot be retried")
		return
	}
	h.db.Exec(r.Context(), `DELETE FROM dead_letter_queue WHERE job_id=$1`, jobID)
	writeJSON(w, http.StatusOK, map[string]string{"message": "queued for retry"})
}

func (h *JobHandler) Purge(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	tag, err := h.db.Exec(r.Context(), `DELETE FROM jobs WHERE id=$1`, jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete job")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func (h *JobHandler) Logs(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	limit, offset := pageParams(r)
	rows, err := h.db.Query(r.Context(),
		`SELECT id, level, message, metadata, logged_at
		 FROM job_logs WHERE job_id=$1 ORDER BY logged_at DESC LIMIT $2 OFFSET $3`,
		jobID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()
	type logRow struct {
		ID       int64           `json:"id"`
		Level    string          `json:"level"`
		Message  string          `json:"message"`
		Metadata json.RawMessage `json:"metadata"`
		LoggedAt time.Time       `json:"logged_at"`
	}
	logs := []logRow{}
	for rows.Next() {
		var l logRow
		rows.Scan(&l.ID, &l.Level, &l.Message, &l.Metadata, &l.LoggedAt)
		logs = append(logs, l)
	}
	writeJSON(w, http.StatusOK, logs)
}

func (h *JobHandler) Executions(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	rows, err := h.db.Query(r.Context(),
		`SELECT id, worker_id, attempt_number, status::text, started_at, completed_at, duration_ms, error_message
		 FROM job_executions WHERE job_id=$1 ORDER BY attempt_number DESC`, jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()
	type execRow struct {
		ID            string     `json:"id"`
		WorkerID      *string    `json:"worker_id,omitempty"`
		AttemptNumber int        `json:"attempt_number"`
		Status        string     `json:"status"`
		StartedAt     time.Time  `json:"started_at"`
		CompletedAt   *time.Time `json:"completed_at,omitempty"`
		DurationMs    *int       `json:"duration_ms,omitempty"`
		ErrorMessage  *string    `json:"error_message,omitempty"`
	}
	execs := []execRow{}
	for rows.Next() {
		var e execRow
		rows.Scan(&e.ID, &e.WorkerID, &e.AttemptNumber, &e.Status, &e.StartedAt,
			&e.CompletedAt, &e.DurationMs, &e.ErrorMessage)
		execs = append(execs, e)
	}
	writeJSON(w, http.StatusOK, execs)
}
