package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tushar/dis-job-queue/api/internal/workflow"
	"github.com/tushar/dis-job-queue/shared/events"
)

type JobHandler struct {
	db  *pgxpool.Pool
	bus *events.Publisher
}

func NewJobHandler(db *pgxpool.Pool, bus *events.Publisher) *JobHandler {
	return &JobHandler{db: db, bus: bus}
}

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
	Shard          int             `json:"shard"`
	PartitionKey   *string         `json:"partition_key,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
}

const jobColumns = `id, queue_id, type, payload, status::text, priority, max_attempts, attempt_count,
	scheduled_at, run_at, timeout_secs, cron_expression, batch_id, idempotency_key,
	tags, last_error, shard, partition_key, created_at, updated_at, completed_at`

func scanJob(row pgx.Row, j *jobRow) error {
	return row.Scan(&j.ID, &j.QueueID, &j.Type, &j.Payload, &j.Status, &j.Priority,
		&j.MaxAttempts, &j.AttemptCount, &j.ScheduledAt, &j.RunAt, &j.TimeoutSecs,
		&j.CronExpression, &j.BatchID, &j.IdempotencyKey, &j.Tags, &j.LastError,
		&j.Shard, &j.PartitionKey, &j.CreatedAt, &j.UpdatedAt, &j.CompletedAt)
}

const shardExpr = `CASE WHEN q.shard_count <= 1 THEN 0
	ELSE mod(hashtext(COALESCE(%s::text, %s::text)) & 2147483647, q.shard_count) END`

func (h *JobHandler) List(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	limit, offset := pageParams(r)
	status := r.URL.Query().Get("status")

	where := `WHERE queue_id=$1`
	args := []any{queueID}
	if status != "" {
		where += ` AND status=$2::job_status`
		args = append(args, status)
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT `+jobColumns+` FROM jobs `+where+
			fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2),
		append(args, limit, offset)...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	jobs := []jobRow{}
	for rows.Next() {
		var j jobRow
		if err := scanJob(rows, &j); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		jobs = append(jobs, j)
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	var total int
	h.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM jobs `+where, args...).Scan(&total)
	writeJSON(w, http.StatusOK, paginated[jobRow]{Data: jobs, Total: total, Limit: limit, Offset: offset})
}

func (h *JobHandler) HandledTypes(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	types, err := handledTypes(r.Context(), h.db, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read worker capabilities")
		return
	}
	writeJSON(w, http.StatusOK, types)
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
	PartitionKey   *string         `json:"partition_key"`
	Ref            string          `json:"ref"`
	DependsOn      []string        `json:"depends_on"`
}

func (req *createJobRequest) normalize() {
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
}

func (req *createJobRequest) validate() error {
	if req.Type == "" {
		return errors.New("type is required")
	}
	if req.Priority < 1 || req.Priority > 10 {
		return errors.New("priority must be between 1 and 10")
	}
	if req.MaxAttempts < 1 {
		return errors.New("max_attempts must be at least 1")
	}
	if req.TimeoutSecs < 1 {
		return errors.New("timeout_secs must be at least 1")
	}
	return nil
}

func (req *createJobRequest) schedule() (runAt time.Time, status string, nextRunAt *time.Time) {
	runAt, status = time.Now(), "queued"
	if req.ScheduledAt != nil && req.ScheduledAt.After(time.Now()) {
		runAt, status = *req.ScheduledAt, "scheduled"
	}
	if status == "scheduled" && req.CronExpression != nil {
		nextRunAt = &runAt
	}
	return
}

type queueTarget struct {
	ProjectID  string
	Paused     bool
	ShardCount int
}

func (h *JobHandler) queueTarget(ctx context.Context, queueID string) (queueTarget, error) {
	var t queueTarget
	err := h.db.QueryRow(ctx,
		`SELECT project_id, paused, shard_count FROM queues WHERE id=$1`, queueID,
	).Scan(&t.ProjectID, &t.Paused, &t.ShardCount)
	return t, err
}

func (h *JobHandler) Create(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")

	var req createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.normalize()
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	target, err := h.queueTarget(r.Context(), queueID)
	if err != nil {
		writeError(w, http.StatusNotFound, "queue not found")
		return
	}

	runAt, status, nextRunAt := req.schedule()

	deps := dedupe(req.DependsOn)
	if len(deps) > workflow.MaxDependenciesPerJob {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("at most %d dependencies allowed", workflow.MaxDependenciesPerJob))
		return
	}
	if len(deps) > 0 {
		states, err := h.dependencyStates(r.Context(), target.ProjectID, deps)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve dependencies")
			return
		}
		if code, msg := validateDependencies(deps, states); code != 0 {
			writeError(w, code, msg)
			return
		}
		if !allCompleted(states) && status == "queued" {
			status = "blocked"
		}
	}

	jobID := uuid.New().String()

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback(r.Context())

	var inserted string
	err = tx.QueryRow(r.Context(),
		`INSERT INTO jobs (id, queue_id, type, payload, status, priority, max_attempts, scheduled_at,
		   run_at, timeout_secs, cron_expression, idempotency_key, tags, next_run_at, partition_key, shard)
		 SELECT $1::uuid,$2,$3,$4,$5::job_status,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,
		   `+fmt.Sprintf(shardExpr, "$15", "$1")+`
		 FROM queues q WHERE q.id = $2
		 ON CONFLICT (idempotency_key) DO NOTHING
		 RETURNING id`,
		jobID, queueID, req.Type, []byte(req.Payload), status, req.Priority, req.MaxAttempts,
		req.ScheduledAt, runAt, req.TimeoutSecs, req.CronExpression, req.IdempotencyKey,
		req.Tags, nextRunAt, req.PartitionKey,
	).Scan(&inserted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "duplicate idempotency_key")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create job: "+err.Error())
		return
	}

	if err := insertDependencies(r.Context(), tx, jobID, deps); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record dependencies: "+err.Error())
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	if status == "queued" && !target.Paused {
		h.bus.Publish(r.Context(), events.Event{
			Type: events.JobEnqueued, ProjectID: target.ProjectID,
			QueueID: queueID, JobID: jobID, JobType: req.Type, Status: status,
		})
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id": jobID, "status": status, "depends_on": deps,
	})
}

func (h *JobHandler) CreateBatch(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")

	var reqs []createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
		writeError(w, http.StatusBadRequest, "array of jobs required")
		return
	}
	if len(reqs) == 0 {
		writeError(w, http.StatusBadRequest, "array of jobs required")
		return
	}
	if len(reqs) > 1000 {
		writeError(w, http.StatusBadRequest, "maximum 1000 jobs per batch")
		return
	}

	target, err := h.queueTarget(r.Context(), queueID)
	if err != nil {
		writeError(w, http.StatusNotFound, "queue not found")
		return
	}

	nodes := make([]workflow.Node, len(reqs))
	for i := range reqs {
		reqs[i].normalize()
		if err := reqs[i].validate(); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("job at index %d: %s", i, err.Error()))
			return
		}
		nodes[i] = workflow.Node{Ref: reqs[i].Ref, DependsOn: dedupe(reqs[i].DependsOn)}
	}

	graph, err := workflow.Resolve(nodes)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	externalIDs := []string{}
	for _, ids := range graph.External {
		externalIDs = append(externalIDs, ids...)
	}
	externalIDs = dedupe(externalIDs)

	externalStates := map[string]string{}
	if len(externalIDs) > 0 {
		externalStates, err = h.dependencyStates(r.Context(), target.ProjectID, externalIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve dependencies")
			return
		}
		if code, msg := validateDependencies(externalIDs, externalStates); code != 0 {
			writeError(w, code, msg)
			return
		}
	}

	batchID := uuid.New().String()
	ids := make([]string, len(reqs))
	for i := range reqs {
		ids[i] = uuid.New().String()
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback(r.Context())

	type created struct {
		id, status, jobType string
	}
	accepted := make([]created, 0, len(reqs))
	skipped := 0

	for i := range reqs {
		req := &reqs[i]
		runAt, status, nextRunAt := req.schedule()

		blocked := len(graph.Internal[i]) > 0
		for _, ext := range graph.External[i] {
			if externalStates[ext] != "completed" {
				blocked = true
			}
		}
		if blocked && status == "queued" {
			status = "blocked"
		}

		var id string
		err := tx.QueryRow(r.Context(),
			`INSERT INTO jobs (id, queue_id, type, payload, status, priority, max_attempts, scheduled_at,
			   run_at, timeout_secs, batch_id, idempotency_key, tags, cron_expression, next_run_at,
			   partition_key, shard)
			 SELECT $1::uuid,$2,$3,$4,$5::job_status,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
			   `+fmt.Sprintf(shardExpr, "$16", "$1")+`
			 FROM queues q WHERE q.id = $2
			 ON CONFLICT (idempotency_key) DO NOTHING
			 RETURNING id`,
			ids[i], queueID, req.Type, []byte(req.Payload), status, req.Priority, req.MaxAttempts,
			req.ScheduledAt, runAt, req.TimeoutSecs, batchID, req.IdempotencyKey, req.Tags,
			req.CronExpression, nextRunAt, req.PartitionKey,
		).Scan(&id)
		switch {
		case err == nil:
			accepted = append(accepted, created{id: id, status: status, jobType: req.Type})
		case errors.Is(err, pgx.ErrNoRows):
			skipped++
			ids[i] = ""
		default:
			writeError(w, http.StatusInternalServerError, "failed to create batch: "+err.Error())
			return
		}
	}

	for i := range reqs {
		if ids[i] == "" {
			continue
		}
		deps := make([]string, 0, len(graph.Internal[i])+len(graph.External[i]))
		for _, j := range graph.Internal[i] {
			if ids[j] == "" {
				writeError(w, http.StatusConflict,
					fmt.Sprintf("job at index %d depends on ref %q which was skipped as a duplicate idempotency_key", i, reqs[j].Ref))
				return
			}
			deps = append(deps, ids[j])
		}
		deps = append(deps, graph.External[i]...)
		if err := insertDependencies(r.Context(), tx, ids[i], deps); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record dependencies: "+err.Error())
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	jobIDs := make([]string, 0, len(accepted))
	for _, c := range accepted {
		jobIDs = append(jobIDs, c.id)
		if c.status == "queued" && !target.Paused {
			h.bus.Publish(r.Context(), events.Event{
				Type: events.JobEnqueued, ProjectID: target.ProjectID,
				QueueID: queueID, JobID: c.id, JobType: c.jobType, Status: c.status,
			})
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"batch_id": batchID,
		"count":    len(jobIDs),
		"skipped":  skipped,
		"job_ids":  jobIDs,
	})
}

func (h *JobHandler) dependencyStates(ctx context.Context, projectID string, ids []string) (map[string]string, error) {
	rows, err := h.db.Query(ctx,
		`SELECT j.id, j.status::text
		 FROM jobs j
		 JOIN queues q ON q.id = j.queue_id
		 WHERE j.id = ANY($1) AND q.project_id = $2`,
		ids, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	states := make(map[string]string, len(ids))
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, err
		}
		states[id] = status
	}
	return states, rows.Err()
}

func validateDependencies(requested []string, states map[string]string) (int, string) {
	missing := []string{}
	for _, id := range requested {
		if _, ok := states[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return http.StatusBadRequest,
			"unknown dependency job ids (or they belong to another project): " + strings.Join(missing, ", ")
	}
	for id, status := range states {
		if status == "dead" || status == "cancelled" {
			return http.StatusConflict,
				fmt.Sprintf("dependency %s is in terminal state %q and can never complete", id, status)
		}
	}
	return 0, ""
}

func allCompleted(states map[string]string) bool {
	for _, s := range states {
		if s != "completed" {
			return false
		}
	}
	return true
}

func insertDependencies(ctx context.Context, tx pgx.Tx, jobID string, deps []string) error {
	if len(deps) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO job_dependencies (job_id, depends_on_job_id)
		 SELECT $1, d FROM unnest($2::uuid[]) AS d
		 ON CONFLICT DO NOTHING`,
		jobID, deps)
	return err
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func (h *JobHandler) Get(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	var j jobRow
	err := scanJob(h.db.QueryRow(r.Context(), `SELECT `+jobColumns+` FROM jobs WHERE id=$1`, jobID), &j)
	if err != nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, j)
}

type dependencyEdge struct {
	JobID  string `json:"job_id"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

func (h *JobHandler) Dependencies(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")

	upstream, err := h.edges(r.Context(),
		`SELECT j.id, j.type, j.status::text
		 FROM job_dependencies d JOIN jobs j ON j.id = d.depends_on_job_id
		 WHERE d.job_id = $1 ORDER BY j.created_at`, jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	downstream, err := h.edges(r.Context(),
		`SELECT j.id, j.type, j.status::text
		 FROM job_dependencies d JOIN jobs j ON j.id = d.job_id
		 WHERE d.depends_on_job_id = $1 ORDER BY j.created_at`, jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	blockedBy := []string{}
	for _, e := range upstream {
		if e.Status != "completed" {
			blockedBy = append(blockedBy, e.JobID)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"job_id":     jobID,
		"depends_on": upstream,
		"dependents": downstream,
		"blocked_by": blockedBy,
		"satisfied":  len(blockedBy) == 0,
	})
}

func (h *JobHandler) edges(ctx context.Context, query, jobID string) ([]dependencyEdge, error) {
	rows, err := h.db.Query(ctx, query, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []dependencyEdge{}
	for rows.Next() {
		var e dependencyEdge
		if err := rows.Scan(&e.JobID, &e.Type, &e.Status); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (h *JobHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	tag, err := h.db.Exec(r.Context(),
		`UPDATE jobs SET status='cancelled', updated_at=now()
		 WHERE id=$1 AND status IN ('queued','scheduled','blocked')`, jobID)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "job cannot be cancelled")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "cancelled"})
}

func (h *JobHandler) Retry(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")

	var status, queueID, projectID, jobType string
	var paused bool
	err := h.db.QueryRow(r.Context(),
		`UPDATE jobs SET
		   status = CASE WHEN EXISTS (
		       SELECT 1 FROM job_dependencies d JOIN jobs p ON p.id = d.depends_on_job_id
		       WHERE d.job_id = jobs.id AND p.status <> 'completed'
		     ) THEN 'blocked'::job_status ELSE 'queued'::job_status END,
		   attempt_count=0, last_error=NULL, completed_at=NULL, claimed_by=NULL,
		   claimed_at=NULL, run_at=now(), updated_at=now()
		 WHERE id=$1 AND status IN ('failed','dead','cancelled')
		 RETURNING status::text, queue_id, type`, jobID,
	).Scan(&status, &queueID, &jobType)
	if err != nil {
		writeError(w, http.StatusConflict, "job cannot be retried")
		return
	}

	h.db.Exec(r.Context(), `DELETE FROM dead_letter_queue WHERE job_id=$1`, jobID)

	if status == "queued" {
		if err := h.db.QueryRow(r.Context(),
			`SELECT p.id, q.paused FROM queues q JOIN projects p ON p.id=q.project_id WHERE q.id=$1`,
			queueID,
		).Scan(&projectID, &paused); err == nil && !paused {
			h.bus.Publish(r.Context(), events.Event{
				Type: events.JobEnqueued, ProjectID: projectID,
				QueueID: queueID, JobID: jobID, JobType: jobType, Status: status,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "queued for retry", "status": status})
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
		if err := rows.Scan(&l.ID, &l.Level, &l.Message, &l.Metadata, &l.LoggedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
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
		if err := rows.Scan(&e.ID, &e.WorkerID, &e.AttemptNumber, &e.Status, &e.StartedAt,
			&e.CompletedAt, &e.DurationMs, &e.ErrorMessage); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		execs = append(execs, e)
	}
	writeJSON(w, http.StatusOK, execs)
}
