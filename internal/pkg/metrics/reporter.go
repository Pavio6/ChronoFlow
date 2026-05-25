package metrics

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	CallbackRequestsTotal   = "chronoflow_callback_requests_total"
	CallbackDurationSeconds = "chronoflow_callback_duration_seconds"
	Records                 = "chronoflow_records"
	PendingOverdueRecords   = "chronoflow_pending_overdue_records"
	RunningStaleRecords     = "chronoflow_running_stale_records"
	RedisQueueItems         = "chronoflow_redis_queue_items"

	LabelResult = "result"
	LabelStatus = "status"

	ResultSuccess = "success"
	ResultFailed  = "failed"
)

// Reporter owns the low-cardinality metrics exported for monitoring.
type Reporter struct {
	callbackRequests   *prometheus.CounterVec
	callbackDuration   *prometheus.HistogramVec
	records            *prometheus.GaugeVec
	pendingOverdue     prometheus.Gauge
	runningStale       prometheus.Gauge
	redisQueueItems    prometheus.Gauge
	execTotalCount     atomic.Int64
	execSuccessCount   atomic.Int64
	execFailedCount    atomic.Int64
	triggerTotalCount  atomic.Int64
	durationTotalNanos atomic.Int64
	durationCount      atomic.Int64
}

// Snapshot is the current process-local summary used by the existing monitoring page.
type Snapshot struct {
	ExecTotal        int64   `json:"exec_total"`
	ExecSuccess      int64   `json:"exec_success"`
	ExecFailed       int64   `json:"exec_failed"`
	TriggerTotal     int64   `json:"trigger_total"`
	AvgDurationMs    float64 `json:"avg_duration_ms"`
	SuccessRate      float64 `json:"success_rate"`
	LastCollectedMsg string  `json:"last_collected_msg"`
}

var (
	reporterInstance *Reporter
	reporterOnce     sync.Once
)

// NewReporter returns the singleton reporter and registers metrics exactly once.
func NewReporter() *Reporter {
	reporterOnce.Do(func() {
		reporterInstance = &Reporter{
			callbackRequests: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: CallbackRequestsTotal,
				Help: "Total number of callback requests by result.",
			}, []string{LabelResult}),
			callbackDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
				Name:    CallbackDurationSeconds,
				Help:    "Callback request latency in seconds by result.",
				Buckets: prometheus.DefBuckets,
			}, []string{LabelResult}),
			records: promauto.NewGaugeVec(prometheus.GaugeOpts{
				Name: Records,
				Help: "Current timer record count by status.",
			}, []string{LabelStatus}),
			pendingOverdue: promauto.NewGauge(prometheus.GaugeOpts{
				Name: PendingOverdueRecords,
				Help: "Current number of overdue PENDING timer records.",
			}),
			runningStale: promauto.NewGauge(prometheus.GaugeOpts{
				Name: RunningStaleRecords,
				Help: "Current number of stale RUNNING timer records.",
			}),
			redisQueueItems: promauto.NewGauge(prometheus.GaugeOpts{
				Name: RedisQueueItems,
				Help: "Current total number of items in ChronoFlow Redis queues.",
			}),
		}
		for _, result := range []string{ResultSuccess, ResultFailed} {
			reporterInstance.callbackRequests.WithLabelValues(result).Add(0)
			reporterInstance.callbackDuration.WithLabelValues(result)
		}
	})
	return reporterInstance
}

// ReportCallback records one completed outbound callback.
func (r *Reporter) ReportCallback(result string, duration time.Duration) {
	r.callbackRequests.WithLabelValues(result).Inc()
	r.callbackDuration.WithLabelValues(result).Observe(duration.Seconds())
	r.execTotalCount.Add(1)
	r.durationTotalNanos.Add(duration.Nanoseconds())
	r.durationCount.Add(1)
	if result == ResultSuccess {
		r.execSuccessCount.Add(1)
		return
	}
	r.execFailedCount.Add(1)
}

// ReportTrigger maintains the process-local summary for the current management page.
func (r *Reporter) ReportTrigger() {
	r.triggerTotalCount.Add(1)
}

func (r *Reporter) SetRecordCount(status string, count int64) {
	r.records.WithLabelValues(status).Set(float64(count))
}

func (r *Reporter) SetPendingOverdueRecords(count int64) {
	r.pendingOverdue.Set(float64(count))
}

func (r *Reporter) SetRunningStaleRecords(count int64) {
	r.runningStale.Set(float64(count))
}

func (r *Reporter) SetRedisQueueItems(count int64) {
	r.redisQueueItems.Set(float64(count))
}

// Snapshot returns aggregated callback values for the current server process.
func (r *Reporter) Snapshot() Snapshot {
	execTotal := r.execTotalCount.Load()
	durationCount := r.durationCount.Load()
	success := r.execSuccessCount.Load()

	var avgDuration float64
	if durationCount > 0 {
		avgDuration = float64(r.durationTotalNanos.Load()) / float64(durationCount) / float64(time.Millisecond)
	}

	var successRate float64
	if execTotal > 0 {
		successRate = float64(success) / float64(execTotal)
	}

	return Snapshot{
		ExecTotal:        execTotal,
		ExecSuccess:      success,
		ExecFailed:       r.execFailedCount.Load(),
		TriggerTotal:     r.triggerTotalCount.Load(),
		AvgDurationMs:    avgDuration,
		SuccessRate:      successRate,
		LastCollectedMsg: "runtime snapshot",
	}
}

func (r *Reporter) GetHandler() http.Handler {
	return promhttp.Handler()
}
