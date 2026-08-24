//go:build integration

package handler_test

import (
	"fmt"
	"net/http"
	"testing"
)

func memberWithRole(t *testing.T, role string) string {
	t.Helper()

	token, userID := newToken(t)

	var email string
	if err := testPool.QueryRow(t.Context(), `SELECT email FROM users WHERE id=$1`, userID).Scan(&email); err != nil {
		t.Fatalf("look up new user: %v", err)
	}

	resp := mustDo("POST", fmt.Sprintf("/api/v1/orgs/%s/members", testOrgID),
		mustJSON(map[string]any{"email": email, "role": role}), adminToken)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add %s member: status %d (%s)", role, resp.StatusCode, readBody(resp))
	}

	t.Cleanup(func() {
		mustDo("DELETE", fmt.Sprintf("/api/v1/orgs/%s/members/%s", testOrgID, userID), nil, adminToken)
	})
	return token
}

func TestRBACEnforcesRolePerRoute(t *testing.T) {
	queueID := createQueue(t, "rbac-"+testRunID, nil)
	jobID, _ := createJob(t, queueID, map[string]any{"type": "workflow_step"})

	tokens := map[string]string{
		"viewer": memberWithRole(t, "viewer"),
		"member": memberWithRole(t, "member"),
		"admin":  memberWithRole(t, "admin"),
	}

	cases := []struct {
		name          string
		method, path  string
		body          map[string]any
		allowedFor    []string
		forbiddenFor  []string
		successStatus int
	}{
		{
			name: "read a queue", method: "GET", path: "/api/v1/queues/" + queueID,
			allowedFor: []string{"viewer", "member", "admin"}, successStatus: http.StatusOK,
		},
		{
			name: "read a job", method: "GET", path: "/api/v1/jobs/" + jobID,
			allowedFor: []string{"viewer", "member", "admin"}, successStatus: http.StatusOK,
		},
		{
			name: "read queue metrics", method: "GET", path: "/api/v1/queues/" + queueID + "/metrics",
			allowedFor: []string{"viewer", "member", "admin"}, successStatus: http.StatusOK,
		},
		{
			name: "pause a queue", method: "POST", path: "/api/v1/queues/" + queueID + "/pause",
			allowedFor: []string{"member", "admin"}, forbiddenFor: []string{"viewer"},
			successStatus: http.StatusOK,
		},
		{
			name: "resume a queue", method: "POST", path: "/api/v1/queues/" + queueID + "/resume",
			allowedFor: []string{"member", "admin"}, forbiddenFor: []string{"viewer"},
			successStatus: http.StatusOK,
		},
		{
			name: "reconfigure a queue", method: "PUT", path: "/api/v1/queues/" + queueID,
			body:       map[string]any{"concurrency_limit": 7},
			allowedFor: []string{"admin"}, forbiddenFor: []string{"viewer", "member"},
			successStatus: http.StatusOK,
		},
		{
			name: "create a retry policy", method: "POST",
			path:       fmt.Sprintf("/api/v1/projects/%s/retry-policies", testProjectID),
			body:       map[string]any{"name": "rbac-policy-" + testRunID, "strategy": "fixed"},
			allowedFor: []string{"admin"}, forbiddenFor: []string{"viewer", "member"},
			successStatus: http.StatusCreated,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, role := range tc.forbiddenFor {
				var body = mustJSONOrNil(tc.body)
				resp := mustDo(tc.method, tc.path, body, tokens[role])
				if resp.StatusCode != http.StatusForbidden {
					t.Errorf("%s %s as %s: status = %d, want 403", tc.method, tc.path, role, resp.StatusCode)
				}
				readBody(resp)
			}
			for _, role := range tc.allowedFor {
				var body = mustJSONOrNil(tc.body)
				resp := mustDo(tc.method, tc.path, body, tokens[role])
				if resp.StatusCode == http.StatusForbidden {
					t.Errorf("%s %s as %s was forbidden, that role should be allowed", tc.method, tc.path, role)
				}
				readBody(resp)
			}
		})
	}
}

func TestRBACCreateJobRequiresMember(t *testing.T) {
	queueID := createQueue(t, "rbac-create-"+testRunID, nil)
	viewer := memberWithRole(t, "viewer")
	member := memberWithRole(t, "member")

	resp := mustDo("POST", "/api/v1/queues/"+queueID+"/jobs",
		mustJSON(map[string]any{"type": "workflow_step"}), viewer)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer creating a job: status = %d, want 403 (%s)", resp.StatusCode, readBody(resp))
	}

	resp = mustDo("POST", "/api/v1/queues/"+queueID+"/jobs",
		mustJSON(map[string]any{"type": "workflow_step"}), member)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("member creating a job: status = %d, want 201 (%s)", resp.StatusCode, readBody(resp))
	}
}

func TestRBACPurgeRequiresAdmin(t *testing.T) {
	queueID := createQueue(t, "rbac-purge-"+testRunID, nil)
	jobID, _ := createJob(t, queueID, map[string]any{"type": "workflow_step"})

	member := memberWithRole(t, "member")
	resp := mustDo("DELETE", "/api/v1/jobs/"+jobID+"/purge", nil, member)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member purging a job: status = %d, want 403", resp.StatusCode)
	}

	resp = mustDo("DELETE", "/api/v1/jobs/"+jobID+"/purge", nil, adminToken)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner purging a job: status = %d, want 200 (%s)", resp.StatusCode, readBody(resp))
	}
}

func TestRBACHidesResourcesFromNonMembers(t *testing.T) {
	queueID := createQueue(t, "rbac-tenant-"+testRunID, nil)
	jobID, _ := createJob(t, queueID, map[string]any{"type": "workflow_step"})

	outsider, _ := newToken(t)

	probes := []struct{ method, path string }{
		{"GET", "/api/v1/queues/" + queueID},
		{"GET", "/api/v1/jobs/" + jobID},
		{"GET", "/api/v1/jobs/" + jobID + "/logs"},
		{"GET", "/api/v1/jobs/" + jobID + "/executions"},
		{"GET", "/api/v1/jobs/" + jobID + "/dependencies"},
		{"GET", "/api/v1/queues/" + queueID + "/jobs"},
		{"GET", "/api/v1/queues/" + queueID + "/stats"},
		{"GET", "/api/v1/queues/" + queueID + "/dlq"},
		{"GET", "/api/v1/projects/" + testProjectID},
		{"GET", "/api/v1/projects/" + testProjectID + "/queues"},
		{"GET", "/api/v1/projects/" + testProjectID + "/workers"},
		{"GET", "/api/v1/projects/" + testProjectID + "/metrics"},
		{"GET", "/api/v1/orgs/" + testOrgID},
		{"GET", "/api/v1/orgs/" + testOrgID + "/members"},
		{"POST", "/api/v1/jobs/" + jobID + "/retry"},
		{"DELETE", "/api/v1/jobs/" + jobID},
	}

	for _, p := range probes {
		resp := mustDo(p.method, p.path, nil, outsider)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s as an outsider: status = %d, want 404 (body %s)",
				p.method, p.path, resp.StatusCode, readBody(resp))
			continue
		}
		readBody(resp)
	}
}

func TestRBACRejectsUnauthenticatedAccess(t *testing.T) {
	queueID := createQueue(t, "rbac-anon-"+testRunID, nil)

	resp := mustDo("GET", "/api/v1/queues/"+queueID, nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous read: status = %d, want 401", resp.StatusCode)
	}
}

func TestRBACAdminCannotGrantOwner(t *testing.T) {
	admin := memberWithRole(t, "admin")
	_, targetID := newToken(t)

	var email string
	testPool.QueryRow(t.Context(), `SELECT email FROM users WHERE id=$1`, targetID).Scan(&email)

	resp := mustDo("POST", fmt.Sprintf("/api/v1/orgs/%s/members", testOrgID),
		mustJSON(map[string]any{"email": email, "role": "owner"}), admin)

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin granting owner: status = %d, want 403 (%s)", resp.StatusCode, readBody(resp))
	}
}

func TestRBACAdminCannotRemoveOwner(t *testing.T) {
	admin := memberWithRole(t, "admin")

	var ownerID string
	if err := testPool.QueryRow(t.Context(),
		`SELECT user_id FROM organization_members WHERE org_id=$1 AND role='owner' LIMIT 1`,
		testOrgID).Scan(&ownerID); err != nil {
		t.Fatalf("find owner: %v", err)
	}

	resp := mustDo("DELETE", fmt.Sprintf("/api/v1/orgs/%s/members/%s", testOrgID, ownerID), nil, admin)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin removing the owner: status = %d, want 403 (%s)", resp.StatusCode, readBody(resp))
	}
}

func TestRBACRejectsUnknownRole(t *testing.T) {
	_, targetID := newToken(t)
	var email string
	testPool.QueryRow(t.Context(), `SELECT email FROM users WHERE id=$1`, targetID).Scan(&email)

	resp := mustDo("POST", fmt.Sprintf("/api/v1/orgs/%s/members", testOrgID),
		mustJSON(map[string]any{"email": email, "role": "superuser"}), adminToken)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("granting an unknown role: status = %d, want 400 (%s)", resp.StatusCode, readBody(resp))
	}
}

func TestRBACLastOwnerCannotBeRemoved(t *testing.T) {
	var ownerID string
	if err := testPool.QueryRow(t.Context(),
		`SELECT user_id FROM organization_members WHERE org_id=$1 AND role='owner' LIMIT 1`,
		testOrgID).Scan(&ownerID); err != nil {
		t.Fatalf("find owner: %v", err)
	}

	var owners int
	testPool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM organization_members WHERE org_id=$1 AND role='owner'`,
		testOrgID).Scan(&owners)
	if owners != 1 {
		t.Skipf("org has %d owners, this test needs exactly one", owners)
	}

	resp := mustDo("DELETE", fmt.Sprintf("/api/v1/orgs/%s/members/%s", testOrgID, ownerID), nil, adminToken)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("removing the last owner: status = %d, want 409 (%s)", resp.StatusCode, readBody(resp))
	}
}
