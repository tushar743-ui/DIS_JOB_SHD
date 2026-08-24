package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tushar/dis-job-queue/shared/events"
	"github.com/tushar/dis-job-queue/shared/shard"
)

type QueueHandler struct {
	db  *pgxpool.Pool
	bus *events.Publisher
}

func NewQueueHandler(db *pgxpool.Pool, bus *events.Publisher) *QueueHandler {
	return &QueueHandler{db: db, bus: bus}
}

type queueRow struct {
	ID               string    `json:"id"`
	ProjectID        string    `json:"project_id"`
	Name             string    `json:"name"`
	Description      *string   `json:"description,omitempty"`
	Priority         int       `json:"priority"`
	ConcurrencyLimit int       `json:"concurrency_limit"`
	Paused           bool      `json:"paused"`
	ShardCount       int       `json:"shard_count"`
	RetryPolicyID    *string   `json:"retry_policy_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (h *QueueHandler) List(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	rows, err := h.db.Query(r.Context(),
		`SELECT id, project_id, name, description, priority, concurrency_limit, paused, shard_count, retry_policy_id, created_at, updated_at
		 FROM queues WHERE project_id=$1 ORDER BY name`, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()
	queues := []queueRow{}
	for rows.Next() {
		var q queueRow
		if err := rows.Scan(&q.ID, &q.ProjectID, &q.Name, &q.Description, &q.Priority,
			&q.ConcurrencyLimit, &q.Paused, &q.ShardCount, &q.RetryPolicyID, &q.CreatedAt, &q.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		queues = append(queues, q)
	}
	writeJSON(w, http.StatusOK, queues)
}

func (h *QueueHandler) Create(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	var req struct {
		Name             string  `json:"name"`
		Description      string  `json:"description"`
		Priority         int     `json:"priority"`
		ConcurrencyLimit int     `json:"concurrency_limit"`
		ShardCount       int     `json:"shard_count"`
		RetryPolicyID    *string `json:"retry_policy_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	if req.Priority == 0 {
		req.Priority = 5
	}
	if req.ConcurrencyLimit == 0 {
		req.ConcurrencyLimit = 10
	}
	if req.ShardCount == 0 {
		req.ShardCount = 1
	}
	if req.Priority < 1 || req.Priority > 10 {
		writeError(w, http.StatusBadRequest, "priority must be between 1 and 10")
		return
	}
	if req.ConcurrencyLimit < 1 {
		writeError(w, http.StatusBadRequest, "concurrency_limit must be at least 1")
		return
	}
	if req.ShardCount < 1 || req.ShardCount > shard.MaxShards {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("shard_count must be between 1 and %d", shard.MaxShards))
		return
	}

	var id string
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO queues (project_id, name, description, priority, concurrency_limit, shard_count, retry_policy_id)
		 VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,$7) RETURNING id`,
		projectID, req.Name, req.Description, req.Priority, req.ConcurrencyLimit, req.ShardCount, req.RetryPolicyID,
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusConflict, "queue name already exists in this project")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *QueueHandler) Get(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	var q queueRow
	if err := h.db.QueryRow(r.Context(),
		`SELECT id, project_id, name, description, priority, concurrency_limit, paused, shard_count, retry_policy_id, created_at, updated_at
		 FROM queues WHERE id=$1`, queueID,
	).Scan(&q.ID, &q.ProjectID, &q.Name, &q.Description, &q.Priority,
		&q.ConcurrencyLimit, &q.Paused, &q.ShardCount, &q.RetryPolicyID, &q.CreatedAt, &q.UpdatedAt); err != nil {
		writeError(w, http.StatusNotFound, "queue not found")
		return
	}
	writeJSON(w, http.StatusOK, q)
}

func (h *QueueHandler) Update(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	var req struct {
		Description      *string `json:"description"`
		Priority         *int    `json:"priority"`
		ConcurrencyLimit *int    `json:"concurrency_limit"`
		ShardCount       *int    `json:"shard_count"`
		RetryPolicyID    *string `json:"retry_policy_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Priority != nil && (*req.Priority < 1 || *req.Priority > 10) {
		writeError(w, http.StatusBadRequest, "priority must be between 1 and 10")
		return
	}
	if req.ConcurrencyLimit != nil && *req.ConcurrencyLimit < 1 {
		writeError(w, http.StatusBadRequest, "concurrency_limit must be at least 1")
		return
	}
	if req.ShardCount != nil && (*req.ShardCount < 1 || *req.ShardCount > shard.MaxShards) {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("shard_count must be between 1 and %d", shard.MaxShards))
		return
	}
	tag, err := h.db.Exec(r.Context(),
		`UPDATE queues SET
		   description    = COALESCE($1, description),
		   priority       = COALESCE($2, priority),
		   concurrency_limit = COALESCE($3, concurrency_limit),
		   shard_count    = COALESCE($4, shard_count),
		   retry_policy_id = COALESCE($5, retry_policy_id),
		   updated_at = now()
		 WHERE id=$6`,
		req.Description, req.Priority, req.ConcurrencyLimit, req.ShardCount, req.RetryPolicyID, queueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "queue not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "updated"})
}

func (h *QueueHandler) Delete(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	h.db.Exec(r.Context(), `DELETE FROM queues WHERE id=$1`, queueID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *QueueHandler) Pause(w http.ResponseWriter, r *http.Request) {
	h.setPaused(w, r, true, events.QueuePaused, "paused")
}

func (h *QueueHandler) Resume(w http.ResponseWriter, r *http.Request) {
	h.setPaused(w, r, false, events.QueueResumed, "resumed")
}

func (h *QueueHandler) setPaused(w http.ResponseWriter, r *http.Request, paused bool, kind events.Type, message string) {
	queueID := chi.URLParam(r, "queueID")

	var projectID string
	err := h.db.QueryRow(r.Context(),
		`UPDATE queues SET paused=$1, updated_at=now() WHERE id=$2 RETURNING project_id`,
		paused, queueID,
	).Scan(&projectID)
	if err != nil {
		writeError(w, http.StatusNotFound, "queue not found")
		return
	}

	h.bus.Publish(r.Context(), events.Event{
		Type: kind, ProjectID: projectID, QueueID: queueID,
	})
	writeJSON(w, http.StatusOK, map[string]string{"message": message})
}

func (h *QueueHandler) Stats(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	type stats struct {
		QueueID  string         `json:"queue_id"`
		ByStatus map[string]int `json:"by_status"`
		ByShard  map[string]int `json:"by_shard,omitempty"`
		Total    int            `json:"total"`
	}
	rows, err := h.db.Query(r.Context(),
		`SELECT status::text, COUNT(*) FROM jobs WHERE queue_id=$1 GROUP BY status`, queueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()
	s := stats{QueueID: queueID, ByStatus: map[string]int{}}
	for rows.Next() {
		var st string
		var cnt int
		if err := rows.Scan(&st, &cnt); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		s.ByStatus[st] = cnt
		s.Total += cnt
	}

	shardRows, err := h.db.Query(r.Context(),
		`SELECT shard, COUNT(*) FROM jobs
		 WHERE queue_id=$1 AND status IN ('queued','blocked','running','claimed')
		 GROUP BY shard ORDER BY shard`, queueID)
	if err == nil {
		defer shardRows.Close()
		s.ByShard = map[string]int{}
		for shardRows.Next() {
			var sh, cnt int
			if err := shardRows.Scan(&sh, &cnt); err == nil {
				s.ByShard[strconv.Itoa(sh)] = cnt
			}
		}
	}

	writeJSON(w, http.StatusOK, s)
}
