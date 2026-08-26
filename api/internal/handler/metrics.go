package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MetricsHandler struct{ db *pgxpool.Pool }

func NewMetricsHandler(db *pgxpool.Pool) *MetricsHandler { return &MetricsHandler{db: db} }

type throughputPoint struct {
	Hour      time.Time `json:"hour"`
	Completed int       `json:"completed"`
	Failed    int       `json:"failed"`
}

const (
	scopeQueue   = `j.queue_id = $1`
	scopeProject = `j.queue_id IN (SELECT id FROM queues WHERE project_id = $1)`

	throughputSQL = `
		SELECT h.hr,
		       COUNT(e.id) FILTER (WHERE e.status = 'completed') AS completed,
		       COUNT(e.id) FILTER (WHERE e.status IN ('failed','timed_out')) AS failed
		FROM generate_series(
		         to_timestamp(floor(extract(epoch FROM now()) / $2::int) * $2::int)
		           - make_interval(secs => $2::int * ($3::int - 1)),
		         to_timestamp(floor(extract(epoch FROM now()) / $2::int) * $2::int),
		         make_interval(secs => $2::int)) AS h(hr)
		LEFT JOIN (
		         SELECT je.id, je.status, je.completed_at
		         FROM job_executions je
		         JOIN jobs j ON j.id = je.job_id
		         WHERE %s AND je.completed_at > now() - make_interval(secs => $2::int * $3::int)
		     ) e ON to_timestamp(floor(extract(epoch FROM e.completed_at) / $2::int) * $2::int) = h.hr
		GROUP BY h.hr
		ORDER BY h.hr`

	avgDurationSQL = `
		SELECT AVG(je.duration_ms)
		FROM job_executions je
		JOIN jobs j ON j.id = je.job_id
		WHERE %s AND je.status = 'completed'
		  AND je.completed_at > now() - make_interval(hours => $2::int)`
)

func rangeHours(r *http.Request) int {
	hours, err := strconv.Atoi(r.URL.Query().Get("hours"))
	if err != nil || hours < 1 {
		return 24
	}
	if hours > 720 {
		return 720
	}
	return hours
}

func bucketing(hours int) (secs, buckets int) {
	switch {
	case hours <= 6:
		return 900, hours * 4
	case hours <= 48:
		return 3600, hours
	default:
		return 86400, (hours + 23) / 24
	}
}

func (h *MetricsHandler) throughput(ctx context.Context, scope, id string, hours int) ([]throughputPoint, error) {
	secs, buckets := bucketing(hours)
	rows, err := h.db.Query(ctx, fmt.Sprintf(throughputSQL, scope), id, secs, buckets)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := []throughputPoint{}
	for rows.Next() {
		var p throughputPoint
		if err := rows.Scan(&p.Hour, &p.Completed, &p.Failed); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

func (h *MetricsHandler) avgDuration(ctx context.Context, scope, id string, hours int) *float64 {
	var avgMs *float64
	h.db.QueryRow(ctx, fmt.Sprintf(avgDurationSQL, scope), id, hours).Scan(&avgMs)
	return avgMs
}

type queueStats struct {
	QueueID   string         `json:"queue_id"`
	QueueName string         `json:"queue_name"`
	ByStatus  map[string]int `json:"by_status"`
}

func (h *MetricsHandler) queueBreakdown(ctx context.Context, projectID string) ([]*queueStats, error) {
	rows, err := h.db.Query(ctx,
		`SELECT q.id, q.name, j.status::text, COUNT(j.id)
		 FROM queues q LEFT JOIN jobs j ON j.queue_id=q.id
		 WHERE q.project_id=$1
		 GROUP BY q.id, q.name, j.status
		 ORDER BY q.name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	statsMap := map[string]*queueStats{}
	result := []*queueStats{}
	for rows.Next() {
		var qid, qname string
		var status *string
		var count int
		if err := rows.Scan(&qid, &qname, &status, &count); err != nil {
			return nil, err
		}
		entry, ok := statsMap[qid]
		if !ok {
			entry = &queueStats{QueueID: qid, QueueName: qname, ByStatus: map[string]int{}}
			statsMap[qid] = entry
			result = append(result, entry)
		}
		if status != nil {
			entry.ByStatus[*status] = count
		}
	}
	return result, rows.Err()
}

func (h *MetricsHandler) activeWorkers(ctx context.Context, projectID string) int {
	var n int
	h.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM workers WHERE project_id=$1 AND status='active'
		   AND last_heartbeat_at > now() - interval '`+liveWorkerWindow+`'`, projectID,
	).Scan(&n)
	return n
}

func (h *MetricsHandler) ProjectMetrics(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	hours := rangeHours(r)
	bucketSecs, _ := bucketing(hours)
	ctx := r.Context()

	var (
		result     []*queueStats
		throughput []throughputPoint
		avgMs      *float64
		workers    int
		queuesErr  error
		tputErr    error
		wg         sync.WaitGroup
	)

	wg.Add(4)
	go func() { defer wg.Done(); result, queuesErr = h.queueBreakdown(ctx, projectID) }()
	go func() { defer wg.Done(); throughput, tputErr = h.throughput(ctx, scopeProject, projectID, hours) }()
	go func() { defer wg.Done(); avgMs = h.avgDuration(ctx, scopeProject, projectID, hours) }()
	go func() { defer wg.Done(); workers = h.activeWorkers(ctx, projectID) }()
	wg.Wait()

	if queuesErr != nil || tputErr != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	completed24h := 0
	for _, p := range throughput {
		completed24h += p.Completed
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"queues":          result,
		"active_workers":  workers,
		"completed_24h":   completed24h,
		"throughput_24h":  throughput,
		"range_hours":     hours,
		"bucket_seconds":  bucketSecs,
		"avg_duration_ms": avgMs,
		"generated_at":    time.Now(),
	})
}

func (h *MetricsHandler) QueueMetrics(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	hours := rangeHours(r)
	bucketSecs, _ := bucketing(hours)

	ctx := r.Context()

	var (
		throughput []throughputPoint
		byStatus   = map[string]int{}
		avgMs      *float64
		tputErr    error
		statusErr  error
		wg         sync.WaitGroup
	)

	wg.Add(3)
	go func() { defer wg.Done(); throughput, tputErr = h.throughput(ctx, scopeQueue, queueID, hours) }()
	go func() { defer wg.Done(); avgMs = h.avgDuration(ctx, scopeQueue, queueID, hours) }()
	go func() {
		defer wg.Done()
		sRows, err := h.db.Query(ctx,
			`SELECT status::text, COUNT(*) FROM jobs WHERE queue_id=$1 GROUP BY status`, queueID)
		if err != nil {
			statusErr = err
			return
		}
		defer sRows.Close()
		for sRows.Next() {
			var st string
			var cnt int
			if err := sRows.Scan(&st, &cnt); err != nil {
				statusErr = err
				return
			}
			byStatus[st] = cnt
		}
		statusErr = sRows.Err()
	}()
	wg.Wait()

	if tputErr != nil || statusErr != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"queue_id":        queueID,
		"by_status":       byStatus,
		"throughput_24h":  throughput,
		"range_hours":     hours,
		"bucket_seconds":  bucketSecs,
		"avg_duration_ms": avgMs,
		"generated_at":    time.Now(),
	})
}
