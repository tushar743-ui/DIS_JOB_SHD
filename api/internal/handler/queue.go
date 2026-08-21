package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type QueueHandler struct{ db *pgxpool.Pool }

func NewQueueHandler(db *pgxpool.Pool) *QueueHandler { return &QueueHandler{db: db} }

type queueRow struct {
	ID               string    `json:"id"`
	ProjectID        string    `json:"project_id"`
	Name             string    `json:"name"`
	Description      *string   `json:"description,omitempty"`
	Priority         int       `json:"priority"`
	ConcurrencyLimit int       `json:"concurrency_limit"`
	Paused           bool      `json:"paused"`
	RetryPolicyID    *string   `json:"retry_policy_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (h *QueueHandler) List(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	rows, err := h.db.Query(r.Context(),
		`SELECT id, project_id, name, description, priority, concurrency_limit, paused, retry_policy_id, created_at, updated_at
		 FROM queues WHERE project_id=$1 ORDER BY name`, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()
	queues := []queueRow{}
	for rows.Next() {
		var q queueRow
		rows.Scan(&q.ID, &q.ProjectID, &q.Name, &q.Description, &q.Priority,
			&q.ConcurrencyLimit, &q.Paused, &q.RetryPolicyID, &q.CreatedAt, &q.UpdatedAt)
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

	var id string
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO queues (project_id, name, description, priority, concurrency_limit, retry_policy_id)
		 VALUES ($1,$2,NULLIF($3,''),$4,$5,$6) RETURNING id`,
		projectID, req.Name, req.Description, req.Priority, req.ConcurrencyLimit, req.RetryPolicyID,
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
		`SELECT id, project_id, name, description, priority, concurrency_limit, paused, retry_policy_id, created_at, updated_at
		 FROM queues WHERE id=$1`, queueID,
	).Scan(&q.ID, &q.ProjectID, &q.Name, &q.Description, &q.Priority,
		&q.ConcurrencyLimit, &q.Paused, &q.RetryPolicyID, &q.CreatedAt, &q.UpdatedAt); err != nil {
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
		RetryPolicyID    *string `json:"retry_policy_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	h.db.Exec(r.Context(),
		`UPDATE queues SET
		   description    = COALESCE($1, description),
		   priority       = COALESCE($2, priority),
		   concurrency_limit = COALESCE($3, concurrency_limit),
		   retry_policy_id = COALESCE($4, retry_policy_id),
		   updated_at = now()
		 WHERE id=$5`,
		req.Description, req.Priority, req.ConcurrencyLimit, req.RetryPolicyID, queueID)
	writeJSON(w, http.StatusOK, map[string]string{"message": "updated"})
}

func (h *QueueHandler) Delete(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	h.db.Exec(r.Context(), `DELETE FROM queues WHERE id=$1`, queueID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *QueueHandler) Pause(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	h.db.Exec(r.Context(), `UPDATE queues SET paused=true, updated_at=now() WHERE id=$1`, queueID)
	writeJSON(w, http.StatusOK, map[string]string{"message": "paused"})
}

func (h *QueueHandler) Resume(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	h.db.Exec(r.Context(), `UPDATE queues SET paused=false, updated_at=now() WHERE id=$1`, queueID)
	writeJSON(w, http.StatusOK, map[string]string{"message": "resumed"})
}

func (h *QueueHandler) Stats(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	type stats struct {
		QueueID   string         `json:"queue_id"`
		ByStatus  map[string]int `json:"by_status"`
		Total     int            `json:"total"`
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
		rows.Scan(&st, &cnt)
		s.ByStatus[st] = cnt
		s.Total += cnt
	}
	writeJSON(w, http.StatusOK, s)
}
