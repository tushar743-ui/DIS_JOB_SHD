package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/tushar/dis-job-queue/api/internal/ai"
	"github.com/tushar/dis-job-queue/api/internal/middleware"
	"github.com/tushar/dis-job-queue/shared/lock"
)

const (
	summaryRateLimit  = 40
	summaryRateWindow = time.Hour
	summaryLockTTL    = 60 * time.Second
	summaryMaxLogs    = 60
)

type FailureSummaryHandler struct {
	db  *pgxpool.Pool
	rdb *redis.Client
	ai  *ai.Summarizer
}

func NewFailureSummaryHandler(db *pgxpool.Pool, rdb *redis.Client, s *ai.Summarizer) *FailureSummaryHandler {
	return &FailureSummaryHandler{db: db, rdb: rdb, ai: s}
}

type failureSummaryRow struct {
	JobID           string    `json:"job_id"`
	Summary         string    `json:"summary"`
	LikelyCause     string    `json:"likely_cause"`
	SuggestedAction string    `json:"suggested_action"`
	Category        string    `json:"category"`
	Confidence      string    `json:"confidence"`
	IsTransient     bool      `json:"is_transient"`
	Model           string    `json:"model"`
	InputTokens     int       `json:"input_tokens"`
	OutputTokens    int       `json:"output_tokens"`
	Stale           bool      `json:"stale"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (h *FailureSummaryHandler) Get(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")

	row, hash, err := h.cached(r.Context(), jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "no failure summary generated for this job yet")
		return
	}

	fc, ferr := h.failureContext(r.Context(), jobID)
	if ferr == nil {
		row.Stale = hash != fc.Fingerprint(h.ai.Model())
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *FailureSummaryHandler) Generate(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")

	if !h.ai.Enabled() {
		writeError(w, http.StatusServiceUnavailable,
			"AI failure summaries are not configured on this deployment (set ANTHROPIC_API_KEY)")
		return
	}

	fc, err := h.failureContext(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if fc.Status != "failed" && fc.Status != "dead" {
		writeError(w, http.StatusConflict,
			"only failed or dead jobs can be summarised, this job is "+fc.Status)
		return
	}

	fingerprint := fc.Fingerprint(h.ai.Model())

	if row, hash, err := h.cached(r.Context(), jobID); err == nil && row != nil && hash == fingerprint {
		writeJSON(w, http.StatusOK, row)
		return
	}

	if err := h.consumeQuota(r.Context(), fc.ProjectID); err != nil {
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}

	held, err := lock.Acquire(r.Context(), h.rdb, "djq:ai:summary:"+jobID, summaryLockTTL)
	if err != nil {
		if errors.Is(err, lock.ErrNotAcquired) {
			writeError(w, http.StatusConflict, "a summary for this job is already being generated")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to acquire generation lock")
		return
	}
	defer held.Release(r.Context())

	summary, err := h.ai.Summarize(r.Context(), fc.FailureContext)
	if err != nil {
		switch {
		case errors.Is(err, ai.ErrDisabled):
			writeError(w, http.StatusServiceUnavailable, err.Error())
		case errors.Is(err, ai.ErrRefused):
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			writeError(w, http.StatusBadGateway, err.Error())
		}
		return
	}

	userID := middleware.UserIDFromContext(r.Context())
	var row failureSummaryRow
	err = h.db.QueryRow(r.Context(),
		`INSERT INTO job_failure_summaries
		   (job_id, summary, likely_cause, suggested_action, category, confidence,
		    is_transient, model, input_hash, input_tokens, output_tokens, generated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		 ON CONFLICT (job_id) DO UPDATE SET
		   summary = EXCLUDED.summary,
		   likely_cause = EXCLUDED.likely_cause,
		   suggested_action = EXCLUDED.suggested_action,
		   category = EXCLUDED.category,
		   confidence = EXCLUDED.confidence,
		   is_transient = EXCLUDED.is_transient,
		   model = EXCLUDED.model,
		   input_hash = EXCLUDED.input_hash,
		   input_tokens = EXCLUDED.input_tokens,
		   output_tokens = EXCLUDED.output_tokens,
		   generated_by = EXCLUDED.generated_by
		 RETURNING job_id, summary, likely_cause, suggested_action, category, confidence,
		           is_transient, model, input_tokens, output_tokens, created_at, updated_at`,
		jobID, summary.Summary, summary.LikelyCause, summary.SuggestedAction, summary.Category,
		summary.Confidence, summary.IsTransient, summary.Model, fingerprint,
		summary.InputTokens, summary.OutputTokens, nullIfEmpty(userID),
	).Scan(&row.JobID, &row.Summary, &row.LikelyCause, &row.SuggestedAction, &row.Category,
		&row.Confidence, &row.IsTransient, &row.Model, &row.InputTokens, &row.OutputTokens,
		&row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store summary: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, row)
}

func (h *FailureSummaryHandler) cached(ctx context.Context, jobID string) (*failureSummaryRow, string, error) {
	var row failureSummaryRow
	var hash string
	err := h.db.QueryRow(ctx,
		`SELECT job_id, summary, likely_cause, suggested_action, category, confidence,
		        is_transient, model, input_tokens, output_tokens, input_hash, created_at, updated_at
		 FROM job_failure_summaries WHERE job_id=$1`, jobID,
	).Scan(&row.JobID, &row.Summary, &row.LikelyCause, &row.SuggestedAction, &row.Category,
		&row.Confidence, &row.IsTransient, &row.Model, &row.InputTokens, &row.OutputTokens,
		&hash, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return &row, hash, nil
}

type scopedFailureContext struct {
	ai.FailureContext
	ProjectID string
}

func (h *FailureSummaryHandler) failureContext(ctx context.Context, jobID string) (scopedFailureContext, error) {
	var fc scopedFailureContext
	var lastError *string
	var payload []byte

	err := h.db.QueryRow(ctx,
		`SELECT j.type, q.name, j.status::text, j.attempt_count, j.max_attempts,
		        j.timeout_secs, j.last_error, j.payload, q.project_id
		 FROM jobs j JOIN queues q ON q.id = j.queue_id
		 WHERE j.id = $1`, jobID,
	).Scan(&fc.JobType, &fc.QueueName, &fc.Status, &fc.AttemptCount, &fc.MaxAttempts,
		&fc.TimeoutSecs, &lastError, &payload, &fc.ProjectID)
	if err != nil {
		return fc, err
	}

	fc.JobID = jobID
	if lastError != nil {
		fc.LastError = *lastError
	}
	fc.Payload = string(payload)

	execRows, err := h.db.Query(ctx,
		`SELECT attempt_number, status::text, duration_ms, error_message, started_at
		 FROM job_executions WHERE job_id=$1 ORDER BY attempt_number`, jobID)
	if err != nil {
		return fc, err
	}
	defer execRows.Close()
	for execRows.Next() {
		var e ai.ExecutionSample
		var msg *string
		if err := execRows.Scan(&e.AttemptNumber, &e.Status, &e.DurationMs, &msg, &e.StartedAt); err != nil {
			return fc, err
		}
		if msg != nil {
			e.ErrorMessage = *msg
		}
		fc.Executions = append(fc.Executions, e)
	}

	logRows, err := h.db.Query(ctx,
		`SELECT level, message, logged_at FROM job_logs
		 WHERE job_id=$1 ORDER BY logged_at DESC LIMIT $2`, jobID, summaryMaxLogs)
	if err != nil {
		return fc, err
	}
	defer logRows.Close()
	for logRows.Next() {
		var l ai.LogSample
		if err := logRows.Scan(&l.Level, &l.Message, &l.LoggedAt); err != nil {
			return fc, err
		}
		fc.Logs = append(fc.Logs, l)
	}
	for i, j := 0, len(fc.Logs)-1; i < j; i, j = i+1, j-1 {
		fc.Logs[i], fc.Logs[j] = fc.Logs[j], fc.Logs[i]
	}

	return fc, nil
}

func (h *FailureSummaryHandler) consumeQuota(ctx context.Context, projectID string) error {
	key := "djq:ai:quota:" + projectID
	count, err := h.rdb.Incr(ctx, key).Result()
	if err != nil {
		return nil
	}
	if count == 1 {
		h.rdb.Expire(ctx, key, summaryRateWindow)
	}
	if count > summaryRateLimit {
		return errors.New("this project has reached its hourly AI summary quota, try again later")
	}
	return nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
