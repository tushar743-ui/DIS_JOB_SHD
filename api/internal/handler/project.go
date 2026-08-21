package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tushar/dis-job-queue/api/internal/middleware"
)

type ProjectHandler struct{ db *pgxpool.Pool }

func NewProjectHandler(db *pgxpool.Pool) *ProjectHandler { return &ProjectHandler{db: db} }

type projectRow struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	rows, err := h.db.Query(r.Context(),
		`SELECT id, org_id, name, slug, created_at FROM projects WHERE org_id=$1 ORDER BY created_at DESC`,
		orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()
	projects := []projectRow{}
	for rows.Next() {
		var p projectRow
		rows.Scan(&p.ID, &p.OrgID, &p.Name, &p.Slug, &p.CreatedAt)
		projects = append(projects, p)
	}
	writeJSON(w, http.StatusOK, projects)
}

func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	var req struct{ Name string `json:"name"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}

	apiKey, keyHash := generateAPIKey()
	slug := slugify(req.Name)

	var projectID string
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO projects (org_id, name, slug, api_key_hash) VALUES ($1,$2,$3,$4) RETURNING id`,
		orgID, req.Name, slug, keyHash,
	).Scan(&projectID)
	if err != nil {
		writeError(w, http.StatusConflict, "project name already exists in this organization")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":      projectID,
		"api_key": apiKey,
		"slug":    slug,
	})
}

func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	var p projectRow
	if err := h.db.QueryRow(r.Context(),
		`SELECT id, org_id, name, slug, created_at FROM projects WHERE id=$1`, projectID,
	).Scan(&p.ID, &p.OrgID, &p.Name, &p.Slug, &p.CreatedAt); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	var req struct{ Name string `json:"name"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	h.db.Exec(r.Context(),
		`UPDATE projects SET name=$1, slug=$2, updated_at=now() WHERE id=$3`,
		req.Name, slugify(req.Name), projectID)
	writeJSON(w, http.StatusOK, map[string]string{"message": "updated"})
}

func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	h.db.Exec(r.Context(), `DELETE FROM projects WHERE id=$1`, projectID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *ProjectHandler) RotateKey(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	apiKey, keyHash := generateAPIKey()
	h.db.Exec(r.Context(),
		`UPDATE projects SET api_key_hash=$1, updated_at=now() WHERE id=$2`, keyHash, projectID)
	writeJSON(w, http.StatusOK, map[string]string{"api_key": apiKey})
}

type retryPolicyRow struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Strategy         string  `json:"strategy"`
	MaxAttempts      int     `json:"max_attempts"`
	InitialDelayMs   int     `json:"initial_delay_ms"`
	MaxDelayMs       int     `json:"max_delay_ms"`
	Multiplier       float64 `json:"multiplier"`
}

func (h *ProjectHandler) ListRetryPolicies(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	rows, err := h.db.Query(r.Context(),
		`SELECT id, name, strategy, max_attempts, initial_delay_ms, max_delay_ms, multiplier
		 FROM retry_policies WHERE project_id=$1 ORDER BY name`, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()
	policies := []retryPolicyRow{}
	for rows.Next() {
		var p retryPolicyRow
		rows.Scan(&p.ID, &p.Name, &p.Strategy, &p.MaxAttempts, &p.InitialDelayMs, &p.MaxDelayMs, &p.Multiplier)
		policies = append(policies, p)
	}
	writeJSON(w, http.StatusOK, policies)
}

func (h *ProjectHandler) CreateRetryPolicy(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	var req struct {
		Name           string  `json:"name"`
		Strategy       string  `json:"strategy"`
		MaxAttempts    int     `json:"max_attempts"`
		InitialDelayMs int     `json:"initial_delay_ms"`
		MaxDelayMs     int     `json:"max_delay_ms"`
		Multiplier     float64 `json:"multiplier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Strategy == "" {
		req.Strategy = "exponential"
	}
	if req.MaxAttempts == 0 {
		req.MaxAttempts = 3
	}
	if req.InitialDelayMs == 0 {
		req.InitialDelayMs = 1000
	}
	if req.MaxDelayMs == 0 {
		req.MaxDelayMs = 60000
	}
	if req.Multiplier == 0 {
		req.Multiplier = 2.0
	}

	var id string
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO retry_policies (project_id, name, strategy, max_attempts, initial_delay_ms, max_delay_ms, multiplier)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		projectID, req.Name, req.Strategy, req.MaxAttempts, req.InitialDelayMs, req.MaxDelayMs, req.Multiplier,
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusConflict, "policy name already exists")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func ProjectIDFromAPIKey(ctx interface{ Value(any) any }) string {
	v, _ := ctx.Value(middleware.ContextUserID).(string)
	return v
}

func generateAPIKey() (plain, hashed string) {
	buf := make([]byte, 32)
	rand.Read(buf)
	plain = "djq_" + hex.EncodeToString(buf)
	hashed = sha256hex(plain)
	return
}
