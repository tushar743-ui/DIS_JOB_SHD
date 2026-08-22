package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MetricsHandler struct{ db *pgxpool.Pool }

func NewMetricsHandler(db *pgxpool.Pool) *MetricsHandler { return &MetricsHandler{db: db} }

func (h *MetricsHandler) ProjectMetrics(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

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
	for rows.Next() {
		var qid, qname string
		var status *string
		var count int
		rows.Scan(&qid, &qname, &status, &count)
		if _, ok := statsMap[qid]; !ok {
			statsMap[qid] = &queueStats{QueueID: qid, QueueName: qname, ByStatus: map[string]int{}}
		}
		if status != nil {
			statsMap[qid].ByStatus[*status] = count
		}
	}

	result := make([]*queueStats, 0, len(statsMap))
	for _, v := range statsMap {
		result = append(result, v)
	}

	var activeWorkers int
	h.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM workers WHERE project_id=$1 AND status='active'`, projectID,
	).Scan(&activeWorkers)

	var completed24h int
	h.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM jobs WHERE queue_id IN (SELECT id FROM queues WHERE project_id=$1)
		 AND status='completed' AND completed_at > now() - interval '24 hours'`, projectID,
	).Scan(&completed24h)

	writeJSON(w, http.StatusOK, map[string]any{
		"queues":          result,
		"active_workers":  activeWorkers,
		"completed_24h":   completed24h,
		"generated_at":    time.Now(),
	})
}

func (h *MetricsHandler) QueueMetrics(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "queueID")

	type throughputPoint struct {
		Hour      time.Time `json:"hour"`
		Completed int       `json:"completed"`
		Failed    int       `json:"failed"`
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT date_trunc('hour', completed_at) AS hr,
		        COUNT(*) FILTER (WHERE status='completed') AS completed,
		        COUNT(*) FILTER (WHERE status IN ('dead','failed')) AS failed
		 FROM jobs
		 WHERE queue_id=$1 AND completed_at > now() - interval '24 hours'
		 GROUP BY hr ORDER BY hr`, queueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	throughput := []throughputPoint{}
	for rows.Next() {
		var p throughputPoint
		rows.Scan(&p.Hour, &p.Completed, &p.Failed)
		throughput = append(throughput, p)
	}

	var avgMs *float64
	h.db.QueryRow(r.Context(),
		`SELECT AVG(duration_ms) FROM job_executions je
		 JOIN jobs j ON j.id=je.job_id
		 WHERE j.queue_id=$1 AND je.status='completed'
		   AND je.started_at > now() - interval '24 hours'`, queueID,
	).Scan(&avgMs)

	var byStatus map[string]int
	sRows, _ := h.db.Query(r.Context(),
		`SELECT status::text, COUNT(*) FROM jobs WHERE queue_id=$1 GROUP BY status`, queueID)
	defer sRows.Close()
	byStatus = map[string]int{}
	for sRows.Next() {
		var st string
		var cnt int
		sRows.Scan(&st, &cnt)
		byStatus[st] = cnt
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"queue_id":       queueID,
		"by_status":      byStatus,
		"throughput_24h": throughput,
		"avg_duration_ms": avgMs,
		"generated_at":   time.Now(),
	})
}
