//go:build integration

package handler_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func createQueue(t *testing.T, name string, extra map[string]any) string {
	t.Helper()
	body := map[string]any{"name": name, "priority": 5, "concurrency_limit": 10}
	for k, v := range extra {
		body[k] = v
	}
	resp := mustDo("POST", fmt.Sprintf("/api/v1/projects/%s/queues", testProjectID), mustJSON(body), adminToken)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create queue %s: status %d (%s)", name, resp.StatusCode, readBody(resp))
	}
	var out map[string]string
	mustDecode(resp, &out)
	t.Cleanup(func() { mustDo("DELETE", "/api/v1/queues/"+out["id"], nil, adminToken) })
	return out["id"]
}

func createJob(t *testing.T, queueID string, body map[string]any) (string, string) {
	t.Helper()
	resp := mustDo("POST", "/api/v1/queues/"+queueID+"/jobs", mustJSON(body), adminToken)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create job: status %d (%s)", resp.StatusCode, readBody(resp))
	}
	var out struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	mustDecode(resp, &out)
	return out.ID, out.Status
}

func jobStatus(t *testing.T, jobID string) string {
	t.Helper()
	resp := mustDo("GET", "/api/v1/jobs/"+jobID, nil, adminToken)
	var out struct {
		Status string `json:"status"`
	}
	mustDecode(resp, &out)
	return out.Status
}

func TestWorkflowDependencyStartsBlocked(t *testing.T) {
	queueID := createQueue(t, "wf-blocked-"+testRunID, nil)

	parentID, parentStatus := createJob(t, queueID, map[string]any{"type": "extract"})
	if parentStatus != "queued" {
		t.Fatalf("parent status = %q, want queued", parentStatus)
	}

	childID, childStatus := createJob(t, queueID, map[string]any{
		"type": "transform", "depends_on": []string{parentID},
	})
	if childStatus != "blocked" {
		t.Fatalf("child status = %q, want blocked while its dependency is pending", childStatus)
	}
	if got := jobStatus(t, childID); got != "blocked" {
		t.Fatalf("persisted child status = %q, want blocked", got)
	}
}

func TestWorkflowDependencyOnCompletedParentStartsQueued(t *testing.T) {
	queueID := createQueue(t, "wf-done-"+testRunID, nil)

	parentID, _ := createJob(t, queueID, map[string]any{"type": "extract"})
	if _, err := testPool.Exec(t.Context(),
		`UPDATE jobs SET status='completed', completed_at=now() WHERE id=$1`, parentID); err != nil {
		t.Fatalf("mark parent completed: %v", err)
	}

	_, childStatus := createJob(t, queueID, map[string]any{
		"type": "transform", "depends_on": []string{parentID},
	})
	if childStatus != "queued" {
		t.Fatalf("child status = %q, want queued when its dependency is already complete", childStatus)
	}
}

func TestWorkflowDependencyGraphEndpoint(t *testing.T) {
	queueID := createQueue(t, "wf-graph-"+testRunID, nil)

	parentID, _ := createJob(t, queueID, map[string]any{"type": "extract"})
	childID, _ := createJob(t, queueID, map[string]any{
		"type": "transform", "depends_on": []string{parentID},
	})

	resp := mustDo("GET", "/api/v1/jobs/"+childID+"/dependencies", nil, adminToken)
	var child struct {
		DependsOn []struct {
			JobID  string `json:"job_id"`
			Status string `json:"status"`
		} `json:"depends_on"`
		BlockedBy []string                         `json:"blocked_by"`
		Satisfied bool                             `json:"satisfied"`
	}
	mustDecode(resp, &child)

	if len(child.DependsOn) != 1 || child.DependsOn[0].JobID != parentID {
		t.Fatalf("child upstream = %+v, want the parent job", child.DependsOn)
	}
	if child.Satisfied || len(child.BlockedBy) != 1 {
		t.Fatalf("child reports satisfied=%v blocked_by=%v, want blocked on the parent",
			child.Satisfied, child.BlockedBy)
	}

	resp = mustDo("GET", "/api/v1/jobs/"+parentID+"/dependencies", nil, adminToken)
	var parent struct {
		Dependents []struct {
			JobID string `json:"job_id"`
		} `json:"dependents"`
		Satisfied  bool                     `json:"satisfied"`
	}
	mustDecode(resp, &parent)

	if len(parent.Dependents) != 1 || parent.Dependents[0].JobID != childID {
		t.Fatalf("parent downstream = %+v, want the child job", parent.Dependents)
	}
	if !parent.Satisfied {
		t.Error("a job with no upstream dependencies should report satisfied")
	}
}

func TestWorkflowRejectsUnknownDependency(t *testing.T) {
	queueID := createQueue(t, "wf-unknown-"+testRunID, nil)

	resp := mustDo("POST", "/api/v1/queues/"+queueID+"/jobs", mustJSON(map[string]any{
		"type": "transform", "depends_on": []string{"11111111-2222-3333-4444-555555555555"},
	}), adminToken)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown dependency (%s)", resp.StatusCode, readBody(resp))
	}
}

func TestWorkflowRejectsDependencyFromAnotherProject(t *testing.T) {
	queueID := createQueue(t, "wf-tenant-"+testRunID, nil)

	otherProject := mustDo("POST", fmt.Sprintf("/api/v1/orgs/%s/projects", testOrgID),
		mustJSON(map[string]any{"name": "Other Project " + testRunID}), adminToken)
	var proj map[string]string
	mustDecode(otherProject, &proj)

	otherQueue := mustDo("POST", fmt.Sprintf("/api/v1/projects/%s/queues", proj["id"]),
		mustJSON(map[string]any{"name": "other"}), adminToken)
	var q map[string]string
	mustDecode(otherQueue, &q)

	foreignID, _ := createJob(t, q["id"], map[string]any{"type": "extract"})
	t.Cleanup(func() { mustDo("DELETE", "/api/v1/projects/"+proj["id"], nil, adminToken) })

	resp := mustDo("POST", "/api/v1/queues/"+queueID+"/jobs", mustJSON(map[string]any{
		"type": "transform", "depends_on": []string{foreignID},
	}), adminToken)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a cross-project dependency (%s)",
			resp.StatusCode, readBody(resp))
	}
}

func TestWorkflowRejectsDependencyOnDeadJob(t *testing.T) {
	queueID := createQueue(t, "wf-dead-"+testRunID, nil)

	deadID, _ := createJob(t, queueID, map[string]any{"type": "extract"})
	if _, err := testPool.Exec(t.Context(),
		`UPDATE jobs SET status='dead', completed_at=now() WHERE id=$1`, deadID); err != nil {
		t.Fatalf("mark dead: %v", err)
	}

	resp := mustDo("POST", "/api/v1/queues/"+queueID+"/jobs", mustJSON(map[string]any{
		"type": "transform", "depends_on": []string{deadID},
	}), adminToken)

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for a dependency that can never complete (%s)",
			resp.StatusCode, readBody(resp))
	}
}

func TestWorkflowBatchBuildsDAGFromRefs(t *testing.T) {
	queueID := createQueue(t, "wf-batch-"+testRunID, nil)

	resp := mustDo("POST", "/api/v1/queues/"+queueID+"/jobs/batch", mustJSON([]map[string]any{
		{"ref": "extract", "type": "extract"},
		{"ref": "transform", "type": "transform", "depends_on": []string{"extract"}},
		{"ref": "load", "type": "load", "depends_on": []string{"transform"}},
		{"ref": "notify", "type": "notify", "depends_on": []string{"load", "extract"}},
	}), adminToken)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("batch status = %d (%s)", resp.StatusCode, readBody(resp))
	}
	var out struct {
		Count  int      `json:"count"`
		JobIDs []string `json:"job_ids"`
	}
	mustDecode(resp, &out)

	if out.Count != 4 {
		t.Fatalf("created %d jobs, want 4", out.Count)
	}

	statuses := make([]string, 4)
	for i, id := range out.JobIDs {
		statuses[i] = jobStatus(t, id)
	}
	if statuses[0] != "queued" {
		t.Errorf("root job status = %q, want queued", statuses[0])
	}
	for i, s := range statuses[1:] {
		if s != "blocked" {
			t.Errorf("downstream job %d status = %q, want blocked", i+1, s)
		}
	}

	dep := mustDo("GET", "/api/v1/jobs/"+out.JobIDs[3]+"/dependencies", nil, adminToken)
	var notify struct {
		DependsOn []struct{ JobID string } `json:"depends_on"`
	}
	mustDecode(dep, &notify)
	if len(notify.DependsOn) != 2 {
		t.Fatalf("notify depends on %d jobs, want 2 (load and extract)", len(notify.DependsOn))
	}
}

func TestWorkflowBatchRejectsCycle(t *testing.T) {
	queueID := createQueue(t, "wf-cycle-"+testRunID, nil)

	resp := mustDo("POST", "/api/v1/queues/"+queueID+"/jobs/batch", mustJSON([]map[string]any{
		{"ref": "a", "type": "extract", "depends_on": []string{"b"}},
		{"ref": "b", "type": "transform", "depends_on": []string{"a"}},
	}), adminToken)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a cyclic workflow", resp.StatusCode)
	}
	if body := readBody(resp); !strings.Contains(body, "cycle") {
		t.Fatalf("error body %q does not mention the cycle", body)
	}

	var count int
	testPool.QueryRow(t.Context(), `SELECT COUNT(*) FROM jobs WHERE queue_id=$1`, queueID).Scan(&count)
	if count != 0 {
		t.Fatalf("%d jobs were persisted despite the batch being rejected", count)
	}
}

func TestShardingAssignsJobsAcrossShards(t *testing.T) {
	queueID := createQueue(t, "shard-spread-"+testRunID, map[string]any{"shard_count": 8})

	for i := range 60 {
		createJob(t, queueID, map[string]any{
			"type":          "workflow_step",
			"partition_key": fmt.Sprintf("tenant-%d", i),
		})
	}

	rows, err := testPool.Query(t.Context(),
		`SELECT shard, COUNT(*) FROM jobs WHERE queue_id=$1 GROUP BY shard`, queueID)
	if err != nil {
		t.Fatalf("query shards: %v", err)
	}
	defer rows.Close()

	distinct := 0
	for rows.Next() {
		var shard, count int
		rows.Scan(&shard, &count)
		if shard < 0 || shard > 7 {
			t.Errorf("job assigned to shard %d, outside the queue's 8 shards", shard)
		}
		distinct++
	}
	if distinct < 4 {
		t.Fatalf("60 jobs landed in only %d of 8 shards, the hash is not spreading load", distinct)
	}
}

func TestShardingIsStableForAPartitionKey(t *testing.T) {
	queueID := createQueue(t, "shard-affinity-"+testRunID, map[string]any{"shard_count": 16})

	shards := map[int]bool{}
	for range 5 {
		id, _ := createJob(t, queueID, map[string]any{
			"type": "workflow_step", "partition_key": "customer-42",
		})
		var shard int
		testPool.QueryRow(t.Context(), `SELECT shard FROM jobs WHERE id=$1`, id).Scan(&shard)
		shards[shard] = true
	}

	if len(shards) != 1 {
		t.Fatalf("the same partition key landed on %d different shards, ordering affinity is broken", len(shards))
	}
}

func TestUnshardedQueueKeepsEveryJobOnShardZero(t *testing.T) {
	queueID := createQueue(t, "shard-off-"+testRunID, nil)

	for i := range 10 {
		createJob(t, queueID, map[string]any{
			"type": "workflow_step", "partition_key": fmt.Sprintf("k-%d", i),
		})
	}

	var distinct int
	testPool.QueryRow(t.Context(),
		`SELECT COUNT(DISTINCT shard) FROM jobs WHERE queue_id=$1`, queueID).Scan(&distinct)
	if distinct != 1 {
		t.Fatalf("an unsharded queue used %d shards, every worker must be able to claim its jobs", distinct)
	}
}

func TestQueueRejectsOutOfRangeShardCount(t *testing.T) {
	for _, count := range []int{-1, 0, 65, 1000} {
		body := map[string]any{"name": fmt.Sprintf("bad-shard-%d-%s", count, testRunID), "shard_count": count}
		resp := mustDo("POST", fmt.Sprintf("/api/v1/projects/%s/queues", testProjectID), mustJSON(body), adminToken)

		if count == 0 {
			if resp.StatusCode != http.StatusCreated {
				t.Errorf("shard_count 0 should default to 1, got %d", resp.StatusCode)
				continue
			}
			var out map[string]string
			mustDecode(resp, &out)
			mustDo("DELETE", "/api/v1/queues/"+out["id"], nil, adminToken)
			continue
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("shard_count %d: status = %d, want 400", count, resp.StatusCode)
		}
	}
}

func TestQueueStatsReportShardDistribution(t *testing.T) {
	queueID := createQueue(t, "shard-stats-"+testRunID, map[string]any{"shard_count": 4})
	for i := range 12 {
		createJob(t, queueID, map[string]any{
			"type": "workflow_step", "partition_key": fmt.Sprintf("p%d", i),
		})
	}

	resp := mustDo("GET", "/api/v1/queues/"+queueID+"/stats", nil, adminToken)
	var stats struct {
		ByShard map[string]int `json:"by_shard"`
		Total   int            `json:"total"`
	}
	mustDecode(resp, &stats)

	if stats.Total != 12 {
		t.Fatalf("stats total = %d, want 12", stats.Total)
	}
	if len(stats.ByShard) == 0 {
		t.Fatal("stats did not report any shard distribution for a sharded queue")
	}
}

func TestFeaturesEndpointAdvertisesCapabilities(t *testing.T) {
	resp := mustDo("GET", "/api/v1/features", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var features map[string]any
	mustDecode(resp, &features)

	for _, key := range []string{"ai_failure_summaries", "live_events", "workflow_dependencies", "queue_sharding", "rbac_roles"} {
		if _, ok := features[key]; !ok {
			t.Errorf("features response is missing %q", key)
		}
	}
}

func TestFailureSummaryRequiresAFailedJob(t *testing.T) {
	queueID := createQueue(t, "ai-state-"+testRunID, nil)
	jobID, _ := createJob(t, queueID, map[string]any{"type": "workflow_step"})

	resp := mustDo("POST", "/api/v1/jobs/"+jobID+"/failure-summary", nil, adminToken)

	switch resp.StatusCode {
	case http.StatusServiceUnavailable:
		t.Skip("ANTHROPIC_API_KEY not configured, summary generation is disabled")
	case http.StatusConflict:
	default:
		t.Fatalf("status = %d, want 409 for a queued job (%s)", resp.StatusCode, readBody(resp))
	}
}

func TestFailureSummaryNotFoundBeforeGeneration(t *testing.T) {
	queueID := createQueue(t, "ai-missing-"+testRunID, nil)
	jobID, _ := createJob(t, queueID, map[string]any{"type": "workflow_step"})

	resp := mustDo("GET", "/api/v1/jobs/"+jobID+"/failure-summary", nil, adminToken)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 before any summary exists", resp.StatusCode)
	}
}

func TestLiveEventsStreamDeliversJobEvents(t *testing.T) {
	queueID := createQueue(t, "live-"+testRunID, nil)

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http") +
		"/api/v1/projects/" + testProjectID + "/events?token=" + adminToken

	conn, resp, err := dialWebSocket(t, wsURL)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("websocket dial failed (status %d): %v", status, err)
	}
	defer conn.CloseNow()

	if got := readEventType(t, conn, 5*time.Second); got != "stream.ready" {
		t.Fatalf("first frame type = %q, want stream.ready", got)
	}

	jobID, _ := createJob(t, queueID, map[string]any{"type": "workflow_step"})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var evt struct {
			Type  string `json:"type"`
			JobID string `json:"job_id"`
		}
		raw := readFrame(t, conn, time.Until(deadline))
		if raw == nil {
			break
		}
		if err := json.Unmarshal(raw, &evt); err != nil {
			continue
		}
		if evt.Type == "job.enqueued" && evt.JobID == jobID {
			return
		}
	}
	t.Fatal("job.enqueued event for the new job never arrived on the live stream")
}

func TestLiveEventsStreamRejectsUnauthenticated(t *testing.T) {
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http") +
		"/api/v1/projects/" + testProjectID + "/events"

	conn, resp, err := dialWebSocket(t, wsURL)
	if err == nil {
		conn.CloseNow()
		t.Fatal("an unauthenticated client was allowed onto the live stream")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func mustJSONOrNil(body map[string]any) io.Reader {
	if body == nil {
		return nil
	}
	return mustJSON(body)
}
