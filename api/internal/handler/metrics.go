package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
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

func (h *MetricsHandler) ProjectMetrics(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	hours := rangeHours(r)
	bucketSecs, _ := bucketing(hours)

	type queueStats struct {
		QueueID   string         `json:"queue_id"`
		QueueName string         `json:"queue_name"`
		ByStatus  map[string]int `json:"by_status"`
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT q.id, q.name, j.status::text, COUNT(j.id)
		 FROM queues q LEFT JOIN jobs j ON j.queue_id=q.id
		 WHERE q.project_id=$1
		 GROUP BY q.id, q.name, j.status
		 ORDER BY q.name`, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	statsMap := map[string]*queueStats{}
	order := []string{}
	for rows.Next() {
		var qid, qname string
		var status *string
		var count int
		rows.Scan(&qid, &qname, &status, &count)
		if _, ok := statsMap[qid]; !ok {
			statsMap[qid] = &queueStats{QueueID: qid, QueueName: qname, ByStatus: map[string]int{}}
			order = append(order, qid)
		}
		if status != nil {
			statsMap[qid].ByStatus[*status] = count
		}
	}

	result := make([]*queueStats, 0, len(statsMap))
	for _, qid := range order {
		result = append(result, statsMap[qid])
	}

	throughput, err := h.throughput(r.Context(), scopeProject, projectID, hours)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	var activeWorkers int
	h.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM workers WHERE project_id=$1 AND status='active'`, projectID,
	).Scan(&activeWorkers)

	completed24h := 0
	for _, p := range throughput {
		completed24h += p.Completed
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"queues":          result,
		"active_workers":  activeWorkers,
		"completed_24h":   completed24h,
		"throughput_24h":  throughput,
		"range_hours":     hours,
		"bucket_seconds":  bucketSecs,
		"avg_duration_ms": h.avgDuration(r.Context(), scopeProject, projectID, hours),
		"generated_at":    time.Now(),
	})
}

func (h *MetricsHandler) QueueMetrics(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")
	hours := rangeHours(r)
	bucketSecs, _ := bucketing(hours)

	throughput, err := h.throughput(r.Context(), scopeQueue, queueID, hours)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	byStatus := map[string]int{}
	sRows, err := h.db.Query(r.Context(),
		`SELECT status::text, COUNT(*) FROM jobs WHERE queue_id=$1 GROUP BY status`, queueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer sRows.Close()
	for sRows.Next() {
		var st string
		var cnt int
		sRows.Scan(&st, &cnt)
		byStatus[st] = cnt
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"queue_id":        queueID,
		"by_status":       byStatus,
		"throughput_24h":  throughput,
		"range_hours":     hours,
		"bucket_seconds":  bucketSecs,
		"avg_duration_ms": h.avgDuration(r.Context(), scopeQueue, queueID, hours),
		"generated_at":    time.Now(),
	})
}
