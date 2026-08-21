package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WorkerHandler struct{ db *pgxpool.Pool }

func NewWorkerHandler(db *pgxpool.Pool) *WorkerHandler { return &WorkerHandler{db: db} }

type workerRow struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	Hostname        string    `json:"hostname"`
	PID             int       `json:"pid"`
	Version         *string   `json:"version,omitempty"`
	Status          string    `json:"status"`
	Concurrency     int       `json:"concurrency"`
	RegisteredAt    time.Time `json:"registered_at"`
	LastHeartbeatAt time.Time `json:"last_heartbeat_at"`
}

func (h *WorkerHandler) List(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	status := r.URL.Query().Get("status")

	query := `SELECT id, project_id, hostname, pid, version, status, concurrency, registered_at, last_heartbeat_at
	          FROM workers WHERE project_id=$1`
	args := []any{projectID}
	if status != "" {
		query += " AND status=$2"
		args = append(args, status)
	}
	query += " ORDER BY last_heartbeat_at DESC"

	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	workers := []workerRow{}
	for rows.Next() {
		var w workerRow
		rows.Scan(&w.ID, &w.ProjectID, &w.Hostname, &w.PID, &w.Version, &w.Status,
			&w.Concurrency, &w.RegisteredAt, &w.LastHeartbeatAt)
		workers = append(workers, w)
	}
	writeJSON(w, http.StatusOK, workers)
}

func (h *WorkerHandler) Get(w http.ResponseWriter, r *http.Request) {
	workerID := chi.URLParam(r, "workerID")
	var wr workerRow
	if err := h.db.QueryRow(r.Context(),
		`SELECT id, project_id, hostname, pid, version, status, concurrency, registered_at, last_heartbeat_at
		 FROM workers WHERE id=$1`, workerID,
	).Scan(&wr.ID, &wr.ProjectID, &wr.Hostname, &wr.PID, &wr.Version, &wr.Status,
		&wr.Concurrency, &wr.RegisteredAt, &wr.LastHeartbeatAt); err != nil {
		writeError(w, http.StatusNotFound, "worker not found")
		return
	}

	hbRows, _ := h.db.Query(r.Context(),
		`SELECT heartbeat_at, jobs_running, jobs_completed FROM worker_heartbeats
		 WHERE worker_id=$1 ORDER BY heartbeat_at DESC LIMIT 10`, workerID)
	defer hbRows.Close()

	type hb struct {
		At            time.Time `json:"at"`
		JobsRunning   int       `json:"jobs_running"`
		JobsCompleted int       `json:"jobs_completed"`
	}
	heartbeats := []hb{}
	for hbRows.Next() {
		var h hb
		hbRows.Scan(&h.At, &h.JobsRunning, &h.JobsCompleted)
		heartbeats = append(heartbeats, h)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"worker":     wr,
		"heartbeats": heartbeats,
	})
}
