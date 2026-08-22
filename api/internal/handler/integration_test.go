//go:build integration

package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tushar/dis-job-queue/api/internal/config"
	"github.com/tushar/dis-job-queue/api/internal/db"
	"github.com/tushar/dis-job-queue/api/internal/router"
)

// shared test state — set up once in TestMain
var (
	testServer    *httptest.Server
	testClient    *http.Client
	adminToken    string // owner of testOrgID / testProjectID
	testOrgID     string
	testProjectID string
	testQueueID   string
	testRunID     string // random suffix to avoid name collisions between runs
)

func TestMain(m *testing.M) {
	if os.Getenv("DATABASE_URL") == "" {
		fmt.Println("SKIP: DATABASE_URL not set — integration tests require a real database")
		os.Exit(0)
	}

	testRunID = fmt.Sprintf("%x", rand.Int31())

	cfg := config.Load()
	pool, err := db.NewPool(cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db connect failed: %v\n", err)
		os.Exit(1)
	}
	rdb := db.NewRedis(cfg.RedisURL)

	testServer = httptest.NewServer(router.New(cfg, pool, rdb))
	testClient = testServer.Client()

	// Register admin user and set up org/project/queue for shared use.
	adminEmail := fmt.Sprintf("admin-%s@djq-test.io", testRunID)
	body := mustJSON(map[string]any{"email": adminEmail, "password": "TestPass1234!", "name": "Test Admin"})
	resp := mustDo("POST", "/api/v1/auth/register", body, "")
	var regResp map[string]string
	mustDecode(resp, &regResp)
	adminToken = regResp["access_token"]

	orgResp := mustDo("POST", "/api/v1/orgs", mustJSON(map[string]any{"name": "Test Org " + testRunID}), adminToken)
	var orgBody map[string]string
	mustDecode(orgResp, &orgBody)
	testOrgID = orgBody["id"]

	projResp := mustDo("POST", fmt.Sprintf("/api/v1/orgs/%s/projects", testOrgID),
		mustJSON(map[string]any{"name": "Test Project " + testRunID}), adminToken)
	var projBody map[string]string
	mustDecode(projResp, &projBody)
	testProjectID = projBody["id"]

	qResp := mustDo("POST", fmt.Sprintf("/api/v1/projects/%s/queues", testProjectID),
		mustJSON(map[string]any{"name": "default", "priority": 5, "concurrency_limit": 10}), adminToken)
	var qBody map[string]string
	mustDecode(qResp, &qBody)
	testQueueID = qBody["id"]

	code := m.Run()

	// Cleanup: delete org (cascades to projects, queues, jobs)
	mustDo("DELETE", fmt.Sprintf("/api/v1/orgs/%s", testOrgID), nil, adminToken)
	testServer.Close()
	pool.Close()
	rdb.Close()

	os.Exit(code)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func mustJSON(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

func mustDo(method, path string, body io.Reader, token string) *http.Response {
	req, err := http.NewRequest(method, testServer.URL+path, body)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := testClient.Do(req)
	if err != nil {
		panic(err)
	}
	return resp
}

func mustDecode(r *http.Response, v any) {
	defer r.Body.Close()
	json.NewDecoder(r.Body).Decode(v)
}

func readBody(r *http.Response) string {
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return string(b)
}

func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body := readBody(resp)
		t.Fatalf("expected HTTP %d, got %d — body: %s", want, resp.StatusCode, body)
	}
}

func newToken(t *testing.T) (token, userID string) {
	t.Helper()
	email := fmt.Sprintf("user-%s-%d@djq-test.io", testRunID, time.Now().UnixNano())
	resp := mustDo("POST", "/api/v1/auth/register",
		mustJSON(map[string]any{"email": email, "password": "TestPass1234!", "name": "Test User"}), "")
	assertStatus(t, resp, http.StatusCreated)
	var body map[string]string
	mustDecode(resp, &body)
	return body["access_token"], body["user_id"]
}

// ─── Auth tests ───────────────────────────────────────────────────────────────

func TestAuth_Register(t *testing.T) {
	email := fmt.Sprintf("reg-%s-%d@djq-test.io", testRunID, time.Now().UnixNano())
	resp := mustDo("POST", "/api/v1/auth/register",
		mustJSON(map[string]any{"email": email, "password": "StrongPass1!", "name": "Reg User"}), "")
	assertStatus(t, resp, http.StatusCreated)
	var body map[string]string
	mustDecode(resp, &body)
	if body["access_token"] == "" {
		t.Fatal("expected access_token in response")
	}
	if body["refresh_token"] == "" {
		t.Fatal("expected refresh_token in response")
	}
}

func TestAuth_Register_DuplicateEmail(t *testing.T) {
	email := fmt.Sprintf("dup-%s-%d@djq-test.io", testRunID, time.Now().UnixNano())
	body := mustJSON(map[string]any{"email": email, "password": "StrongPass1!", "name": "User"})
	mustDo("POST", "/api/v1/auth/register", body, "")

	// second registration with same email
	body2 := mustJSON(map[string]any{"email": email, "password": "StrongPass1!", "name": "User2"})
	resp2 := mustDo("POST", "/api/v1/auth/register", body2, "")
	assertStatus(t, resp2, http.StatusConflict)
}

func TestAuth_Register_ShortPassword(t *testing.T) {
	resp := mustDo("POST", "/api/v1/auth/register",
		mustJSON(map[string]any{"email": "shortpw@djq-test.io", "password": "abc", "name": "X"}), "")
	assertStatus(t, resp, http.StatusBadRequest)
}

func TestAuth_Register_MissingFields(t *testing.T) {
	cases := []map[string]any{
		{"password": "StrongPass1!", "name": "User"},          // missing email
		{"email": "x@x.io", "name": "User"},                  // missing password
		{"email": "x@x.io", "password": "StrongPass1!"},      // missing name
	}
	for _, c := range cases {
		resp := mustDo("POST", "/api/v1/auth/register", mustJSON(c), "")
		assertStatus(t, resp, http.StatusBadRequest)
	}
}

func TestAuth_Login(t *testing.T) {
	email := fmt.Sprintf("login-%s-%d@djq-test.io", testRunID, time.Now().UnixNano())
	mustDo("POST", "/api/v1/auth/register",
		mustJSON(map[string]any{"email": email, "password": "LoginPass1!", "name": "Login User"}), "")

	resp := mustDo("POST", "/api/v1/auth/login",
		mustJSON(map[string]any{"email": email, "password": "LoginPass1!"}), "")
	assertStatus(t, resp, http.StatusOK)
	var body map[string]string
	mustDecode(resp, &body)
	if body["access_token"] == "" {
		t.Fatal("expected access_token")
	}
}

func TestAuth_Login_WrongPassword(t *testing.T) {
	email := fmt.Sprintf("wp-%s-%d@djq-test.io", testRunID, time.Now().UnixNano())
	mustDo("POST", "/api/v1/auth/register",
		mustJSON(map[string]any{"email": email, "password": "CorrectPass1!", "name": "User"}), "")

	resp := mustDo("POST", "/api/v1/auth/login",
		mustJSON(map[string]any{"email": email, "password": "WrongPassword!"}), "")
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestAuth_RefreshToken_Rotation(t *testing.T) {
	email := fmt.Sprintf("ref-%s-%d@djq-test.io", testRunID, time.Now().UnixNano())
	regResp := mustDo("POST", "/api/v1/auth/register",
		mustJSON(map[string]any{"email": email, "password": "RefPass1234!", "name": "Ref User"}), "")
	assertStatus(t, regResp, http.StatusCreated)
	var regBody map[string]string
	mustDecode(regResp, &regBody)
	originalRefresh := regBody["refresh_token"]

	// use refresh token
	ref1 := mustDo("POST", "/api/v1/auth/refresh",
		mustJSON(map[string]string{"refresh_token": originalRefresh}), "")
	assertStatus(t, ref1, http.StatusOK)
	var ref1Body map[string]string
	mustDecode(ref1, &ref1Body)
	if ref1Body["access_token"] == "" {
		t.Fatal("expected new access_token after refresh")
	}

	// replay the original — must be rejected (token rotation)
	ref2 := mustDo("POST", "/api/v1/auth/refresh",
		mustJSON(map[string]string{"refresh_token": originalRefresh}), "")
	assertStatus(t, ref2, http.StatusUnauthorized)
}

func TestAuth_Me(t *testing.T) {
	token, _ := newToken(t)
	resp := mustDo("GET", "/api/v1/auth/me", nil, token)
	assertStatus(t, resp, http.StatusOK)
	var body map[string]any
	mustDecode(resp, &body)
	if body["email"] == nil {
		t.Fatal("expected email in /me response")
	}
}

func TestAuth_NoToken_Returns401(t *testing.T) {
	resp := mustDo("GET", "/api/v1/orgs", nil, "")
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestAuth_InvalidToken_Returns401(t *testing.T) {
	resp := mustDo("GET", "/api/v1/orgs", nil, "not-a-valid-jwt")
	assertStatus(t, resp, http.StatusUnauthorized)
}

// ─── Org tests ────────────────────────────────────────────────────────────────

func TestOrg_CRUD(t *testing.T) {
	token, _ := newToken(t)
	name := "CRUD Org " + testRunID + fmt.Sprint(time.Now().UnixNano())

	// create
	resp := mustDo("POST", "/api/v1/orgs", mustJSON(map[string]any{"name": name}), token)
	assertStatus(t, resp, http.StatusCreated)
	var created map[string]string
	mustDecode(resp, &created)
	orgID := created["id"]
	if orgID == "" {
		t.Fatal("expected org id")
	}
	t.Cleanup(func() {
		mustDo("DELETE", fmt.Sprintf("/api/v1/orgs/%s", orgID), nil, token)
	})

	// get
	get := mustDo("GET", fmt.Sprintf("/api/v1/orgs/%s", orgID), nil, token)
	assertStatus(t, get, http.StatusOK)

	// list — must include our new org
	list := mustDo("GET", "/api/v1/orgs", nil, token)
	assertStatus(t, list, http.StatusOK)
	var orgs []map[string]any
	mustDecode(list, &orgs)
	found := false
	for _, o := range orgs {
		if o["id"] == orgID {
			found = true
		}
	}
	if !found {
		t.Fatal("newly created org not returned in list")
	}

	// update
	upd := mustDo("PUT", fmt.Sprintf("/api/v1/orgs/%s", orgID),
		mustJSON(map[string]any{"name": name + " Updated"}), token)
	assertStatus(t, upd, http.StatusOK)

	// verify update
	get2 := mustDo("GET", fmt.Sprintf("/api/v1/orgs/%s", orgID), nil, token)
	var body map[string]any
	mustDecode(get2, &body)
	if !strings.Contains(fmt.Sprint(body["name"]), "Updated") {
		t.Fatalf("org name not updated, got %v", body["name"])
	}
}

func TestOrg_AddMember(t *testing.T) {
	ownerToken, _ := newToken(t)
	memberToken, _ := newToken(t)
	memberEmail := fmt.Sprintf("mem-%s-%d@djq-test.io", testRunID, time.Now().UnixNano())

	// register member with known email
	mustDo("POST", "/api/v1/auth/register",
		mustJSON(map[string]any{"email": memberEmail, "password": "MemberPass1!", "name": "Member"}), "")

	// create org as owner
	orgResp := mustDo("POST", "/api/v1/orgs",
		mustJSON(map[string]any{"name": "Member Org " + testRunID + fmt.Sprint(time.Now().UnixNano())}), ownerToken)
	var orgBody map[string]string
	mustDecode(orgResp, &orgBody)
	orgID := orgBody["id"]
	t.Cleanup(func() {
		mustDo("DELETE", fmt.Sprintf("/api/v1/orgs/%s", orgID), nil, ownerToken)
	})

	// add member by email
	addResp := mustDo("POST", fmt.Sprintf("/api/v1/orgs/%s/members", orgID),
		mustJSON(map[string]any{"email": memberEmail, "role": "member"}), ownerToken)
	assertStatus(t, addResp, http.StatusOK)

	// member can now list this org
	listResp := mustDo("GET", "/api/v1/orgs", nil, memberToken)
	assertStatus(t, listResp, http.StatusOK)

	// list members
	membersResp := mustDo("GET", fmt.Sprintf("/api/v1/orgs/%s/members", orgID), nil, ownerToken)
	assertStatus(t, membersResp, http.StatusOK)
	var members []map[string]any
	mustDecode(membersResp, &members)
	if len(members) < 2 {
		t.Fatalf("expected at least 2 members, got %d", len(members))
	}
}

// ─── Project tests ────────────────────────────────────────────────────────────

func TestProject_CRUD(t *testing.T) {
	name := "Test Proj " + fmt.Sprint(time.Now().UnixNano())

	resp := mustDo("POST", fmt.Sprintf("/api/v1/orgs/%s/projects", testOrgID),
		mustJSON(map[string]any{"name": name}), adminToken)
	assertStatus(t, resp, http.StatusCreated)
	var created map[string]string
	mustDecode(resp, &created)
	projectID := created["id"]
	apiKey := created["api_key"]
	if !strings.HasPrefix(apiKey, "djq_") {
		t.Fatalf("api key should start with djq_, got %s", apiKey)
	}
	t.Cleanup(func() {
		mustDo("DELETE", fmt.Sprintf("/api/v1/projects/%s", projectID), nil, adminToken)
	})

	// get
	get := mustDo("GET", fmt.Sprintf("/api/v1/projects/%s", projectID), nil, adminToken)
	assertStatus(t, get, http.StatusOK)

	// list
	list := mustDo("GET", fmt.Sprintf("/api/v1/orgs/%s/projects", testOrgID), nil, adminToken)
	assertStatus(t, list, http.StatusOK)

	// rotate key
	rot := mustDo("POST", fmt.Sprintf("/api/v1/projects/%s/rotate-key", projectID), nil, adminToken)
	assertStatus(t, rot, http.StatusOK)
	var rotBody map[string]string
	mustDecode(rot, &rotBody)
	newKey := rotBody["api_key"]
	if newKey == apiKey {
		t.Fatal("rotated key should differ from original")
	}
	if !strings.HasPrefix(newKey, "djq_") {
		t.Fatalf("rotated key should start with djq_, got %s", newKey)
	}
}

func TestProject_DuplicateName_Returns409(t *testing.T) {
	name := "Dup Proj " + fmt.Sprint(time.Now().UnixNano())
	resp1 := mustDo("POST", fmt.Sprintf("/api/v1/orgs/%s/projects", testOrgID),
		mustJSON(map[string]any{"name": name}), adminToken)
	assertStatus(t, resp1, http.StatusCreated)
	var b1 map[string]string
	mustDecode(resp1, &b1)
	t.Cleanup(func() {
		mustDo("DELETE", fmt.Sprintf("/api/v1/projects/%s", b1["id"]), nil, adminToken)
	})

	resp2 := mustDo("POST", fmt.Sprintf("/api/v1/orgs/%s/projects", testOrgID),
		mustJSON(map[string]any{"name": name}), adminToken)
	assertStatus(t, resp2, http.StatusConflict)
}

// ─── Retry policy tests ───────────────────────────────────────────────────────

func TestRetryPolicy_CreateAndList(t *testing.T) {
	policyName := "exp-policy-" + fmt.Sprint(time.Now().UnixNano())
	resp := mustDo("POST", fmt.Sprintf("/api/v1/projects/%s/retry-policies", testProjectID),
		mustJSON(map[string]any{
			"name": policyName, "strategy": "exponential",
			"max_attempts": 5, "initial_delay_ms": 500, "max_delay_ms": 30000, "multiplier": 2.0,
		}), adminToken)
	assertStatus(t, resp, http.StatusCreated)
	var body map[string]string
	mustDecode(resp, &body)
	if body["id"] == "" {
		t.Fatal("expected id in response")
	}

	list := mustDo("GET", fmt.Sprintf("/api/v1/projects/%s/retry-policies", testProjectID), nil, adminToken)
	assertStatus(t, list, http.StatusOK)
	var policies []map[string]any
	mustDecode(list, &policies)
	found := false
	for _, p := range policies {
		if p["name"] == policyName {
			found = true
			if p["strategy"] != "exponential" {
				t.Fatalf("expected strategy=exponential, got %v", p["strategy"])
			}
		}
	}
	if !found {
		t.Fatal("created policy not found in list")
	}
}

// ─── Queue tests ──────────────────────────────────────────────────────────────

func TestQueue_CRUD(t *testing.T) {
	qName := "q-" + fmt.Sprint(time.Now().UnixNano())
	resp := mustDo("POST", fmt.Sprintf("/api/v1/projects/%s/queues", testProjectID),
		mustJSON(map[string]any{"name": qName, "priority": 7, "concurrency_limit": 5}), adminToken)
	assertStatus(t, resp, http.StatusCreated)
	var created map[string]string
	mustDecode(resp, &created)
	qID := created["id"]
	t.Cleanup(func() {
		mustDo("DELETE", fmt.Sprintf("/api/v1/queues/%s", qID), nil, adminToken)
	})

	// get
	get := mustDo("GET", fmt.Sprintf("/api/v1/queues/%s", qID), nil, adminToken)
	assertStatus(t, get, http.StatusOK)
	var qBody map[string]any
	mustDecode(get, &qBody)
	if int(qBody["priority"].(float64)) != 7 {
		t.Fatalf("expected priority=7, got %v", qBody["priority"])
	}

	// list
	list := mustDo("GET", fmt.Sprintf("/api/v1/projects/%s/queues", testProjectID), nil, adminToken)
	assertStatus(t, list, http.StatusOK)

	// update
	upd := mustDo("PUT", fmt.Sprintf("/api/v1/queues/%s", qID),
		mustJSON(map[string]any{"concurrency_limit": 20}), adminToken)
	assertStatus(t, upd, http.StatusOK)

	// stats
	stats := mustDo("GET", fmt.Sprintf("/api/v1/queues/%s/stats", qID), nil, adminToken)
	assertStatus(t, stats, http.StatusOK)
}

func TestQueue_PauseResume(t *testing.T) {
	qName := "pause-q-" + fmt.Sprint(time.Now().UnixNano())
	resp := mustDo("POST", fmt.Sprintf("/api/v1/projects/%s/queues", testProjectID),
		mustJSON(map[string]any{"name": qName}), adminToken)
	assertStatus(t, resp, http.StatusCreated)
	var created map[string]string
	mustDecode(resp, &created)
	qID := created["id"]
	t.Cleanup(func() {
		mustDo("DELETE", fmt.Sprintf("/api/v1/queues/%s", qID), nil, adminToken)
	})

	// pause
	pause := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/pause", qID), nil, adminToken)
	assertStatus(t, pause, http.StatusOK)

	// verify paused
	get := mustDo("GET", fmt.Sprintf("/api/v1/queues/%s", qID), nil, adminToken)
	var qBody map[string]any
	mustDecode(get, &qBody)
	if qBody["paused"] != true {
		t.Fatalf("expected paused=true, got %v", qBody["paused"])
	}

	// resume
	resume := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/resume", qID), nil, adminToken)
	assertStatus(t, resume, http.StatusOK)

	get2 := mustDo("GET", fmt.Sprintf("/api/v1/queues/%s", qID), nil, adminToken)
	var qBody2 map[string]any
	mustDecode(get2, &qBody2)
	if qBody2["paused"] != false {
		t.Fatalf("expected paused=false after resume, got %v", qBody2["paused"])
	}
}

// ─── Job tests ────────────────────────────────────────────────────────────────

func TestJob_Create_Basic(t *testing.T) {
	resp := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID),
		mustJSON(map[string]any{
			"type":    "process_order",
			"payload": map[string]any{"order_id": "ORD-TEST-001"},
			"priority": 5,
		}), adminToken)
	assertStatus(t, resp, http.StatusCreated)
	var body map[string]string
	mustDecode(resp, &body)
	if body["id"] == "" {
		t.Fatal("expected job id")
	}
	if body["status"] != "queued" {
		t.Fatalf("expected status=queued, got %s", body["status"])
	}
}

func TestJob_Create_Scheduled(t *testing.T) {
	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	resp := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID),
		mustJSON(map[string]any{
			"type":         "scheduled_task",
			"payload":      map[string]any{},
			"scheduled_at": future,
		}), adminToken)
	assertStatus(t, resp, http.StatusCreated)
	var body map[string]string
	mustDecode(resp, &body)
	if body["status"] != "scheduled" {
		t.Fatalf("expected status=scheduled for future scheduled_at, got %s", body["status"])
	}
}

func TestJob_Create_Idempotency(t *testing.T) {
	key := fmt.Sprintf("idem-%s-%d", testRunID, time.Now().UnixNano())
	create := func() *http.Response {
		return mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID),
			mustJSON(map[string]any{
				"type":            "idem_job",
				"payload":         map[string]any{},
				"idempotency_key": key,
			}), adminToken)
	}
	r1 := create()
	assertStatus(t, r1, http.StatusCreated)

	r2 := create()
	assertStatus(t, r2, http.StatusConflict)
}

func TestJob_Create_MissingType_Returns400(t *testing.T) {
	resp := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID),
		mustJSON(map[string]any{"payload": map[string]any{"x": 1}}), adminToken)
	assertStatus(t, resp, http.StatusBadRequest)
}

func TestJob_Create_InvalidJSON_Returns400(t *testing.T) {
	req, _ := http.NewRequest("POST",
		testServer.URL+fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID),
		strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, _ := testClient.Do(req)
	assertStatus(t, resp, http.StatusBadRequest)
}

func TestJob_GetByID(t *testing.T) {
	// create
	cr := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID),
		mustJSON(map[string]any{"type": "test_get", "payload": map[string]any{}}), adminToken)
	var cr_body map[string]string
	mustDecode(cr, &cr_body)
	jobID := cr_body["id"]

	// get
	get := mustDo("GET", fmt.Sprintf("/api/v1/jobs/%s", jobID), nil, adminToken)
	assertStatus(t, get, http.StatusOK)
	var jobBody map[string]any
	mustDecode(get, &jobBody)
	if jobBody["id"] != jobID {
		t.Fatalf("expected id=%s, got %v", jobID, jobBody["id"])
	}
	if jobBody["type"] != "test_get" {
		t.Fatalf("expected type=test_get, got %v", jobBody["type"])
	}
}

func TestJob_GetByID_NotFound(t *testing.T) {
	resp := mustDo("GET", "/api/v1/jobs/00000000-0000-0000-0000-000000000000", nil, adminToken)
	assertStatus(t, resp, http.StatusNotFound)
}

func TestJob_ListWithStatusFilter(t *testing.T) {
	// create a scheduled job (has status=scheduled)
	future := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
	mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID),
		mustJSON(map[string]any{"type": "filter_test", "scheduled_at": future}), adminToken)

	// list all
	listAll := mustDo("GET", fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID), nil, adminToken)
	assertStatus(t, listAll, http.StatusOK)
	var allBody map[string]any
	mustDecode(listAll, &allBody)
	if allBody["total"] == nil {
		t.Fatal("expected total field in paginated response")
	}

	// list by status=scheduled
	listSched := mustDo("GET",
		fmt.Sprintf("/api/v1/queues/%s/jobs?status=scheduled", testQueueID), nil, adminToken)
	assertStatus(t, listSched, http.StatusOK)
	var schedBody map[string]any
	mustDecode(listSched, &schedBody)
	items := schedBody["data"].([]any)
	for _, item := range items {
		j := item.(map[string]any)
		if j["status"] != "scheduled" {
			t.Fatalf("list by status=scheduled returned job with status=%v", j["status"])
		}
	}
}

func TestJob_Cancel(t *testing.T) {
	cr := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID),
		mustJSON(map[string]any{"type": "cancel_test", "payload": map[string]any{}}), adminToken)
	var crBody map[string]string
	mustDecode(cr, &crBody)
	jobID := crBody["id"]

	cancel := mustDo("DELETE", fmt.Sprintf("/api/v1/jobs/%s", jobID), nil, adminToken)
	assertStatus(t, cancel, http.StatusOK)

	// verify status
	get := mustDo("GET", fmt.Sprintf("/api/v1/jobs/%s", jobID), nil, adminToken)
	var jobBody map[string]any
	mustDecode(get, &jobBody)
	if jobBody["status"] != "cancelled" {
		t.Fatalf("expected status=cancelled, got %v", jobBody["status"])
	}
}

func TestJob_Cancel_AlreadyRunning_Returns409(t *testing.T) {
	// cancel a non-existent / already cancelled job — should conflict
	cr := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID),
		mustJSON(map[string]any{"type": "cancel_conflict", "payload": map[string]any{}}), adminToken)
	var crBody map[string]string
	mustDecode(cr, &crBody)
	jobID := crBody["id"]

	// cancel once
	mustDo("DELETE", fmt.Sprintf("/api/v1/jobs/%s", jobID), nil, adminToken)
	// cancel again — already cancelled, should 409
	resp := mustDo("DELETE", fmt.Sprintf("/api/v1/jobs/%s", jobID), nil, adminToken)
	assertStatus(t, resp, http.StatusConflict)
}

func TestJob_Retry_FromCancelled(t *testing.T) {
	cr := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID),
		mustJSON(map[string]any{"type": "retry_test", "payload": map[string]any{}}), adminToken)
	var crBody map[string]string
	mustDecode(cr, &crBody)
	jobID := crBody["id"]

	// cancel first
	mustDo("DELETE", fmt.Sprintf("/api/v1/jobs/%s", jobID), nil, adminToken)

	// retry
	retry := mustDo("POST", fmt.Sprintf("/api/v1/jobs/%s/retry", jobID), nil, adminToken)
	assertStatus(t, retry, http.StatusOK)

	// verify queued again
	get := mustDo("GET", fmt.Sprintf("/api/v1/jobs/%s", jobID), nil, adminToken)
	var jobBody map[string]any
	mustDecode(get, &jobBody)
	if jobBody["status"] != "queued" {
		t.Fatalf("expected status=queued after retry, got %v", jobBody["status"])
	}
	if jobBody["attempt_count"].(float64) != 0 {
		t.Fatalf("expected attempt_count=0 after retry, got %v", jobBody["attempt_count"])
	}
}

func TestJob_Retry_AlreadyQueued_Returns409(t *testing.T) {
	cr := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID),
		mustJSON(map[string]any{"type": "retry_queued", "payload": map[string]any{}}), adminToken)
	var crBody map[string]string
	mustDecode(cr, &crBody)
	jobID := crBody["id"]

	// retry a queued job (not failed/dead/cancelled) — must 409
	resp := mustDo("POST", fmt.Sprintf("/api/v1/jobs/%s/retry", jobID), nil, adminToken)
	assertStatus(t, resp, http.StatusConflict)
}

func TestJob_CreateBatch(t *testing.T) {
	jobs := make([]map[string]any, 10)
	for i := range jobs {
		jobs[i] = map[string]any{
			"type":    "batch_job",
			"payload": map[string]any{"seq": i},
		}
	}
	resp := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs/batch", testQueueID),
		mustJSON(jobs), adminToken)
	assertStatus(t, resp, http.StatusCreated)
	var body map[string]any
	mustDecode(resp, &body)
	if int(body["count"].(float64)) != 10 {
		t.Fatalf("expected count=10, got %v", body["count"])
	}
	if body["batch_id"] == "" {
		t.Fatal("expected batch_id")
	}
	jobIDs := body["job_ids"].([]any)
	if len(jobIDs) != 10 {
		t.Fatalf("expected 10 job_ids, got %d", len(jobIDs))
	}
}

func TestJob_CreateBatch_Scheduled(t *testing.T) {
	future := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
	resp := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs/batch", testQueueID),
		mustJSON([]map[string]any{
			{"type": "batch_sched", "payload": map[string]any{}, "scheduled_at": future},
			{"type": "batch_sched", "payload": map[string]any{}, "scheduled_at": future},
		}), adminToken)
	assertStatus(t, resp, http.StatusCreated)
	var body map[string]any
	mustDecode(resp, &body)
	// verify both jobs are scheduled
	ids := body["job_ids"].([]any)
	for _, rawID := range ids {
		get := mustDo("GET", fmt.Sprintf("/api/v1/jobs/%s", rawID), nil, adminToken)
		var jb map[string]any
		mustDecode(get, &jb)
		if jb["status"] != "scheduled" {
			t.Fatalf("batch job with future scheduled_at should be 'scheduled', got %v", jb["status"])
		}
	}
}

func TestJob_CreateBatch_TooLarge_Returns400(t *testing.T) {
	jobs := make([]map[string]any, 1001)
	for i := range jobs {
		jobs[i] = map[string]any{"type": "x", "payload": map[string]any{}}
	}
	resp := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs/batch", testQueueID),
		mustJSON(jobs), adminToken)
	assertStatus(t, resp, http.StatusBadRequest)
}

func TestJob_Logs(t *testing.T) {
	cr := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID),
		mustJSON(map[string]any{"type": "log_test", "payload": map[string]any{}}), adminToken)
	var crBody map[string]string
	mustDecode(cr, &crBody)
	jobID := crBody["id"]

	resp := mustDo("GET", fmt.Sprintf("/api/v1/jobs/%s/logs", jobID), nil, adminToken)
	assertStatus(t, resp, http.StatusOK)
	// empty array is fine — job hasn't run yet
	var logs []any
	mustDecode(resp, &logs)
	_ = logs // just verifying the endpoint works
}

func TestJob_Executions(t *testing.T) {
	cr := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID),
		mustJSON(map[string]any{"type": "exec_test", "payload": map[string]any{}}), adminToken)
	var crBody map[string]string
	mustDecode(cr, &crBody)
	jobID := crBody["id"]

	resp := mustDo("GET", fmt.Sprintf("/api/v1/jobs/%s/executions", jobID), nil, adminToken)
	assertStatus(t, resp, http.StatusOK)
}

// ─── Metrics tests ────────────────────────────────────────────────────────────

func TestMetrics_Project(t *testing.T) {
	resp := mustDo("GET", fmt.Sprintf("/api/v1/projects/%s/metrics", testProjectID), nil, adminToken)
	assertStatus(t, resp, http.StatusOK)
	var body map[string]any
	mustDecode(resp, &body)
	if body["active_workers"] == nil {
		t.Fatal("expected active_workers in metrics")
	}
	if body["completed_24h"] == nil {
		t.Fatal("expected completed_24h in metrics")
	}
	if body["queues"] == nil {
		t.Fatal("expected queues in metrics")
	}
}

func TestMetrics_Queue(t *testing.T) {
	resp := mustDo("GET", fmt.Sprintf("/api/v1/queues/%s/metrics", testQueueID), nil, adminToken)
	assertStatus(t, resp, http.StatusOK)
	var body map[string]any
	mustDecode(resp, &body)
	if body["by_status"] == nil {
		t.Fatal("expected by_status in queue metrics")
	}
	if body["throughput_24h"] == nil {
		t.Fatal("expected throughput_24h in queue metrics")
	}
}

// ─── Worker API tests ─────────────────────────────────────────────────────────

func TestWorker_List(t *testing.T) {
	resp := mustDo("GET", fmt.Sprintf("/api/v1/projects/%s/workers", testProjectID), nil, adminToken)
	assertStatus(t, resp, http.StatusOK)
	var workers []any
	mustDecode(resp, &workers)
	// workers may be empty if none are running — just verify shape
	_ = workers
}

func TestWorker_List_StatusFilter(t *testing.T) {
	resp := mustDo("GET",
		fmt.Sprintf("/api/v1/projects/%s/workers?status=active", testProjectID), nil, adminToken)
	assertStatus(t, resp, http.StatusOK)
}

// ─── DLQ tests ────────────────────────────────────────────────────────────────

func TestDLQ_List(t *testing.T) {
	resp := mustDo("GET", fmt.Sprintf("/api/v1/queues/%s/dlq", testQueueID), nil, adminToken)
	assertStatus(t, resp, http.StatusOK)
	var body map[string]any
	mustDecode(resp, &body)
	if body["data"] == nil {
		t.Fatal("expected data field in paginated DLQ response")
	}
}

// ─── Multi-user isolation test ────────────────────────────────────────────────

func TestMultiUser_OrgIsolation(t *testing.T) {
	// user A creates an org
	tokenA, _ := newToken(t)
	orgRespA := mustDo("POST", "/api/v1/orgs",
		mustJSON(map[string]any{"name": "Org A " + fmt.Sprint(time.Now().UnixNano())}), tokenA)
	var orgA map[string]string
	mustDecode(orgRespA, &orgA)
	orgAID := orgA["id"]
	t.Cleanup(func() {
		mustDo("DELETE", fmt.Sprintf("/api/v1/orgs/%s", orgAID), nil, tokenA)
	})

	// user B should NOT see user A's org
	tokenB, _ := newToken(t)
	listB := mustDo("GET", "/api/v1/orgs", nil, tokenB)
	var orgsB []map[string]any
	mustDecode(listB, &orgsB)
	for _, o := range orgsB {
		if o["id"] == orgAID {
			t.Fatal("user B should not see user A's private org")
		}
	}
}

// ─── Load: 10 users × 10 jobs ─────────────────────────────────────────────────

func TestLoad_MultiUserJobSubmission(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	users := make([]string, 10)
	for i := range users {
		token, _ := newToken(t)
		users[i] = token
	}

	type result struct {
		ok  bool
		err string
	}
	results := make(chan result, len(users)*10)

	for _, token := range users {
		tok := token
		for j := 0; j < 10; j++ {
			go func(seq int) {
				resp := mustDo("POST", fmt.Sprintf("/api/v1/queues/%s/jobs", testQueueID),
					mustJSON(map[string]any{
						"type":    "load_test_job",
						"payload": map[string]any{"seq": seq},
					}), tok)
				if resp.StatusCode == http.StatusCreated {
					results <- result{ok: true}
				} else {
					results <- result{ok: false, err: fmt.Sprintf("HTTP %d", resp.StatusCode)}
				}
				resp.Body.Close()
			}(j)
		}
	}

	passed, failed := 0, 0
	for i := 0; i < len(users)*10; i++ {
		r := <-results
		if r.ok {
			passed++
		} else {
			failed++
			t.Logf("job submission failed: %s", r.err)
		}
	}
	if failed > 0 {
		t.Errorf("%d/%d concurrent job submissions failed", failed, len(users)*10)
	}
	t.Logf("load test: %d/%d submissions succeeded", passed, len(users)*10)
}

// ─── Unused import guard (for context usage) ──────────────────────────────────

var _ = context.Background
