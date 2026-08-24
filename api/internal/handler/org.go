package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tushar/dis-job-queue/api/internal/authz"
	"github.com/tushar/dis-job-queue/api/internal/middleware"
)

type OrgHandler struct{ db *pgxpool.Pool }

func NewOrgHandler(db *pgxpool.Pool) *OrgHandler { return &OrgHandler{db: db} }

type orgRow struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Role      string    `json:"role,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *OrgHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	rows, err := h.db.Query(r.Context(),
		`SELECT o.id, o.name, o.slug, om.role, o.created_at
		 FROM organizations o JOIN organization_members om ON om.org_id=o.id
		 WHERE om.user_id=$1 ORDER BY o.created_at DESC`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	orgs := []orgRow{}
	for rows.Next() {
		var o orgRow
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.Role, &o.CreatedAt); err != nil {
			continue
		}
		orgs = append(orgs, o)
	}
	writeJSON(w, http.StatusOK, orgs)
}

func (h *OrgHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	slug := slugify(req.Name)

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback(r.Context())

	var orgID string
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO organizations (name, slug) VALUES ($1,$2) RETURNING id`,
		req.Name, slug,
	).Scan(&orgID); err != nil {
		writeError(w, http.StatusConflict, "organization name/slug already exists")
		return
	}

	if _, err := tx.Exec(r.Context(),
		`INSERT INTO organization_members (org_id, user_id, role) VALUES ($1,$2,'owner')`,
		orgID, userID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"id": orgID, "slug": slug})
}

func (h *OrgHandler) Get(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	userID := middleware.UserIDFromContext(r.Context())
	var o orgRow
	err := h.db.QueryRow(r.Context(),
		`SELECT o.id, o.name, o.slug, om.role, o.created_at
		 FROM organizations o JOIN organization_members om ON om.org_id=o.id
		 WHERE o.id=$1 AND om.user_id=$2`, orgID, userID,
	).Scan(&o.ID, &o.Name, &o.Slug, &o.Role, &o.CreatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "organization not found")
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (h *OrgHandler) Update(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	_, err := h.db.Exec(r.Context(),
		`UPDATE organizations SET name=$1, slug=$2, updated_at=now() WHERE id=$3`,
		req.Name, slugify(req.Name), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "updated"})
}

func (h *OrgHandler) Delete(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if _, err := h.db.Exec(r.Context(), `DELETE FROM organizations WHERE id=$1`, orgID); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *OrgHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	rows, err := h.db.Query(r.Context(),
		`SELECT u.id, u.email, u.name, om.role, om.joined_at
		 FROM organization_members om JOIN users u ON u.id=om.user_id
		 WHERE om.org_id=$1 ORDER BY om.joined_at`, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()
	type member struct {
		ID       string    `json:"id"`
		Email    string    `json:"email"`
		Name     string    `json:"name"`
		Role     string    `json:"role"`
		JoinedAt time.Time `json:"joined_at"`
	}
	members := []member{}
	for rows.Next() {
		var m member
		rows.Scan(&m.ID, &m.Email, &m.Name, &m.Role, &m.JoinedAt)
		members = append(members, m)
	}
	writeJSON(w, http.StatusOK, members)
}

func (h *OrgHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")

	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		writeError(w, http.StatusBadRequest, "email required")
		return
	}
	if req.Role == "" {
		req.Role = string(authz.RoleMember)
	}
	if !authz.Role(req.Role).Valid() {
		writeError(w, http.StatusBadRequest, "role must be one of viewer, member, admin, owner")
		return
	}

	grant, ok := authz.GrantFrom(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	if !grant.Role.AtLeast(authz.Role(req.Role)) {
		writeError(w, http.StatusForbidden,
			"cannot grant a role higher than your own ("+string(grant.Role)+")")
		return
	}

	var targetID string
	if err := h.db.QueryRow(r.Context(),
		`SELECT id FROM users WHERE email=$1`, req.Email,
	).Scan(&targetID); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	var existing string
	h.db.QueryRow(r.Context(),
		`SELECT role FROM organization_members WHERE org_id=$1 AND user_id=$2`,
		orgID, targetID).Scan(&existing)
	if existing != "" && !grant.Role.AtLeast(authz.Role(existing)) {
		writeError(w, http.StatusForbidden,
			"cannot modify a member whose role ("+existing+") is higher than your own")
		return
	}

	if _, err := h.db.Exec(r.Context(),
		`INSERT INTO organization_members (org_id, user_id, role) VALUES ($1,$2,$3)
		 ON CONFLICT (org_id, user_id) DO UPDATE SET role=EXCLUDED.role`,
		orgID, targetID, req.Role); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "member added", "role": req.Role})
}

func (h *OrgHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	targetID := chi.URLParam(r, "userID")

	grant, ok := authz.GrantFrom(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var targetRole string
	if err := h.db.QueryRow(r.Context(),
		`SELECT role FROM organization_members WHERE org_id=$1 AND user_id=$2`,
		orgID, targetID,
	).Scan(&targetRole); err != nil {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	if !grant.Role.AtLeast(authz.Role(targetRole)) {
		writeError(w, http.StatusForbidden,
			"cannot remove a member whose role ("+targetRole+") is higher than your own")
		return
	}

	if targetRole == string(authz.RoleOwner) {
		var owners int
		h.db.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM organization_members WHERE org_id=$1 AND role='owner'`,
			orgID).Scan(&owners)
		if owners <= 1 {
			writeError(w, http.StatusConflict,
				"cannot remove the last owner of an organization")
			return
		}
	}

	if _, err := h.db.Exec(r.Context(),
		`DELETE FROM organization_members WHERE org_id=$1 AND user_id=$2`, orgID, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, c := range s {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b.WriteRune(c)
		} else if c == ' ' || c == '-' || c == '_' {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
