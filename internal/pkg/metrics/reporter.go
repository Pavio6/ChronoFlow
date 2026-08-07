package metrics

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	SchedulerBatchesTotal   = "chronoflow_scheduler_batches_total"
	SchedulerExecutions     = "chronoflow_scheduler_created_executions_total"
	SchedulerDuplicates     = "chronoflow_scheduler_duplicate_executions_total"
	SchedulerBatchDuration  = "chronoflow_scheduler_batch_duration_seconds"
	OutboxPublishTotal      = "chronoflow_outbox_publish_total"
	OutboxUnpublished       = "chronoflow_outbox_unpublished_count"
	WorkerExecutionsTotal   = "chronoflow_worker_executions_total"
	WorkerDurationSeconds   = "chronoflow_worker_execution_duration_seconds"
	WorkerRetriesTotal      = "chronoflow_worker_retries_total"
	WorkerLeaseLostTotal    = "chronoflow_worker_lease_lost_total"
	WorkerRedeliveriesTotal = "chronoflow_worker_redeliveries_total"
	WorkerPendingMessages   = "chronoflow_worker_pending_messages"
	RecoveryActionsTotal    = "chronoflow_recovery_actions_total"
	RecoveryFailuresTotal   = "chronoflow_recovery_failures_total"
	Executions              = "chronoflow_executions"

	LabelResult = "result"
	LabelStatus = "status"
	LabelAction = "action"

	ResultSuccess = "success"
	ResultFailed  = "failed"
)

// Reporter owns the low-cardinality metrics exported for monitoring.
type Reporter struct {
	schedulerBatches       *prometheus.CounterVec
	schedulerExecutions    prometheus.Counter
	schedulerDuplicates    prometheus.Counter
	schedulerBatchDuration prometheus.Histogram
	outboxPublish          *prometheus.CounterVec
	outboxUnpublished      prometheus.Gauge
	workerExecutions       *prometheus.CounterVec
	workerDuration         *prometheus.HistogramVec
	workerRetries          prometheus.Counter
	workerLeaseLost        prometheus.Counter
	workerRedeliveries     prometheus.Counter
	workerPending          prometheus.Gauge
	recoveryActions        *prometheus.CounterVec
	recoveryFailures       prometheus.Counter
	executions             *prometheus.GaugeVec
}

var (
	reporterInstance *Reporter
	reporterOnce     sync.Once
)

// NewReporter returns the singleton reporter and registers metrics exactly once.
func NewReporter() *Reporter {
	reporterOnce.Do(func() {
		reporterInstance = &Reporter{
			schedulerBatches: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: SchedulerBatchesTotal,
				Help: "Total scheduler batches by result.",
			}, []string{LabelResult}),
			schedulerExecutions: promauto.NewCounter(prometheus.CounterOpts{
				Name: SchedulerExecutions,
				Help: "Total durable executions created by the scheduler.",
			}),
			schedulerDuplicates: promauto.NewCounter(prometheus.CounterOpts{
				Name: SchedulerDuplicates,
				Help: "Total duplicate execution insertions prevented by the unique constraint.",
			}),
			schedulerBatchDuration: promauto.NewHistogram(prometheus.HistogramOpts{
				Name:    SchedulerBatchDuration,
				Help:    "Scheduler transaction batch duration in seconds.",
				Buckets: prometheus.DefBuckets,
			}),
			outboxPublish: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: OutboxPublishTotal,
				Help: "Total Outbox publish attempts by result.",
			}, []string{LabelResult}),
			outboxUnpublished: promauto.NewGauge(prometheus.GaugeOpts{
				Name: OutboxUnpublished,
				Help: "Current number of unpublished Outbox events.",
			}),
			workerExecutions: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: WorkerExecutionsTotal,
				Help: "Total Worker callback attempts by result.",
			}, []string{LabelResult}),
			workerDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
				Name:    WorkerDurationSeconds,
				Help:    "Worker callback attempt duration in seconds.",
				Buckets: prometheus.DefBuckets,
			}, []string{LabelResult}),
			workerRetries: promauto.NewCounter(prometheus.CounterOpts{
				Name: WorkerRetriesTotal,
				Help: "Total execution retries scheduled.",
			}),
			workerLeaseLost: promauto.NewCounter(prometheus.CounterOpts{
				Name: WorkerLeaseLostTotal,
				Help: "Total Worker attempts that lost their MySQL Lease.",
			}),
			workerRedeliveries: promauto.NewCounter(prometheus.CounterOpts{
				Name: WorkerRedeliveriesTotal,
				Help: "Total Redis Pending messages reclaimed by Workers.",
			}),
			workerPending: promauto.NewGauge(prometheus.GaugeOpts{
				Name: WorkerPendingMessages,
				Help: "Current Redis Consumer Group pending message count.",
			}),
			recoveryActions: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: RecoveryActionsTotal,
				Help: "Total durable recovery actions by type.",
			}, []string{LabelAction}),
			recoveryFailures: promauto.NewCounter(prometheus.CounterOpts{
				Name: RecoveryFailuresTotal,
				Help: "Total Reconciler scan or cleanup failures.",
			}),
			executions: promauto.NewGaugeVec(prometheus.GaugeOpts{
				Name: Executions,
				Help: "Current durable execution count by status.",
			}, []string{LabelStatus}),
		}
		for _, result := range []string{ResultSuccess, ResultFailed} {
			reporterInstance.schedulerBatches.WithLabelValues(result).Add(0)
			reporterInstance.outboxPublish.WithLabelValues(result).Add(0)
			reporterInstance.workerExecutions.WithLabelValues(result).Add(0)
			reporterInstance.workerDuration.WithLabelValues(result)
		}
		for _, action := range []string{"reenqueue", "expired_lease", "terminal_failure", "cleanup"} {
			reporterInstance.recoveryActions.WithLabelValues(action).Add(0)
		}
		for _, status := range []string{
			"PENDING",
			"RUNNING",
			"RETRY_WAIT",
			"SUCCESS",
			"FAILED",
			"CANCELLED",
		} {
			reporterInstance.executions.WithLabelValues(status).Set(0)
		}
	})
	return reporterInstance
}

// ReportSchedulerBatch records one scheduler transaction.
func (r *Reporter) ReportSchedulerBatch(
	_ int,
	executions int,
	duplicates int,
	duration time.Duration,
	success bool,
) {
	result := ResultFailed
	if success {
		result = ResultSuccess
	}
	r.schedulerBatches.WithLabelValues(result).Inc()
	r.schedulerExecutions.Add(float64(executions))
	r.schedulerDuplicates.Add(float64(duplicates))
	r.schedulerBatchDuration.Observe(duration.Seconds())
}

// ReportOutboxPublish records one Redis Stream publish attempt.
func (r *Reporter) ReportOutboxPublish(success bool) {
	result := ResultFailed
	if success {
		result = ResultSuccess
	}
	r.outboxPublish.WithLabelValues(result).Inc()
}

func (r *Reporter) SetOutboxUnpublished(count int64) {
	r.outboxUnpublished.Set(float64(count))
}

func (r *Reporter) ReportWorkerExecution(result string, duration time.Duration) {
	r.workerExecutions.WithLabelValues(result).Inc()
	r.workerDuration.WithLabelValues(result).Observe(duration.Seconds())
}

func (r *Reporter) ReportWorkerRetry() {
	r.workerRetries.Inc()
}

func (r *Reporter) ReportWorkerLeaseLost() {
	r.workerLeaseLost.Inc()
}

func (r *Reporter) ReportWorkerRedelivery() {
	r.workerRedeliveries.Inc()
}

func (r *Reporter) SetWorkerPending(count int64) {
	r.workerPending.Set(float64(count))
}

func (r *Reporter) ReportRecoveryAction(action string, count int) {
	r.recoveryActions.WithLabelValues(action).Add(float64(count))
}

func (r *Reporter) ReportRecoveryFailure() {
	r.recoveryFailures.Inc()
}

func (r *Reporter) SetExecutionCount(status string, count int64) {
	r.executions.WithLabelValues(status).Set(float64(count))
}

func (r *Reporter) GetHandler() http.Handler {
	return promhttp.Handler()
}
