package authz

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tushar/dis-job-queue/api/internal/middleware"
)

type Role string

const (
	RoleViewer Role = "viewer"
	RoleMember Role = "member"
	RoleAdmin  Role = "admin"
	RoleOwner  Role = "owner"
)

var rank = map[Role]int{
	RoleViewer: 1,
	RoleMember: 2,
	RoleAdmin:  3,
	RoleOwner:  4,
}

func (r Role) Valid() bool { return rank[r] > 0 }

func (r Role) AtLeast(min Role) bool { return rank[r] >= rank[min] }

func Roles() []Role { return []Role{RoleViewer, RoleMember, RoleAdmin, RoleOwner} }

type ctxKey string

const grantKey ctxKey = "authz_grant"

type Grant struct {
	OrgID  string
	Role   Role
	UserID string
}

func GrantFrom(ctx context.Context) (Grant, bool) {
	g, ok := ctx.Value(grantKey).(Grant)
	return g, ok
}

type resource struct {
	param string
	query string
}

var resources = []resource{
	{"orgID", `SELECT o.id, om.role
	           FROM organizations o
	           JOIN organization_members om ON om.org_id = o.id AND om.user_id = $2
	           WHERE o.id = $1`},

	{"projectID", `SELECT p.org_id, om.role
	               FROM projects p
	               JOIN organization_members om ON om.org_id = p.org_id AND om.user_id = $2
	               WHERE p.id = $1`},

	{"queueID", `SELECT p.org_id, om.role
	             FROM queues q
	             JOIN projects p ON p.id = q.project_id
	             JOIN organization_members om ON om.org_id = p.org_id AND om.user_id = $2
	             WHERE q.id = $1`},

	{"jobID", `SELECT p.org_id, om.role
	           FROM jobs j
	           JOIN queues q ON q.id = j.queue_id
	           JOIN projects p ON p.id = q.project_id
	           JOIN organization_members om ON om.org_id = p.org_id AND om.user_id = $2
	           WHERE j.id = $1`},

	{"workerID", `SELECT p.org_id, om.role
	              FROM workers w
	              JOIN projects p ON p.id = w.project_id
	              JOIN organization_members om ON om.org_id = p.org_id AND om.user_id = $2
	              WHERE w.id = $1`},

	{"dlqID", `SELECT p.org_id, om.role
	           FROM dead_letter_queue d
	           JOIN queues q ON q.id = d.queue_id
	           JOIN projects p ON p.id = q.project_id
	           JOIN organization_members om ON om.org_id = p.org_id AND om.user_id = $2
	           WHERE d.id = $1`},
}

var errNoResource = errors.New("authz: no addressable resource in route")

type Authorizer struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Authorizer { return &Authorizer{db: db} }

func (a *Authorizer) Require(min Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := middleware.UserIDFromContext(r.Context())
			if userID == "" {
				deny(w, http.StatusUnauthorized, "authentication required")
				return
			}

			grant, err := a.resolve(r, userID)
			switch {
			case errors.Is(err, errNoResource):
				deny(w, http.StatusInternalServerError, "route is not scoped to a resource")
				return
			case errors.Is(err, pgx.ErrNoRows):
				deny(w, http.StatusNotFound, "resource not found")
				return
			case err != nil:
				deny(w, http.StatusInternalServerError, "authorization check failed")
				return
			}

			if !grant.Role.AtLeast(min) {
				deny(w, http.StatusForbidden,
					"requires role "+string(min)+" or higher, caller has "+string(grant.Role))
				return
			}

			ctx := context.WithValue(r.Context(), grantKey, grant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func (a *Authorizer) resolve(r *http.Request, userID string) (Grant, error) {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return Grant{}, errNoResource
	}

	for _, res := range resources {
		id := rctx.URLParam(res.param)
		if id == "" {
			continue
		}
		var orgID string
		var role Role
		err := a.db.QueryRow(r.Context(), res.query, id, userID).Scan(&orgID, &role)
		if err != nil {
			return Grant{}, err
		}
		return Grant{OrgID: orgID, Role: role, UserID: userID}, nil
	}

	return Grant{}, errNoResource
}

func deny(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":` + quote(msg) + "}\n"))
}

func quote(s string) string {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\\':
			out = append(out, '\\', c)
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			if c < 0x20 {
				continue
			}
			out = append(out, c)
		}
	}
	return string(append(out, '"'))
}
