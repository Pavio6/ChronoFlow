package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// 监控指标名称
const (
	TaskExecTotal     = "chronoflow_task_exec_total"
	TaskExecDuration  = "chronoflow_task_exec_duration_seconds"
	TaskExecSuccess   = "chronoflow_task_exec_success_total"
	TaskExecFailed    = "chronoflow_task_exec_failed_total"
	TaskExecRetry     = "chronoflow_task_exec_retry_total"
	TaskTriggerTotal  = "chronoflow_task_trigger_total"
	TaskQueueSize     = "chronoflow_task_queue_size"
)

// 标签
const (
	LabelTaskID = "task_id"
	LabelStatus = "status"
	LabelApp    = "app"
)

// Reporter 监控上报器
type Reporter struct {
	// 任务执行总数
	taskExecTotal *prometheus.CounterVec
	// 任务执行延迟
	taskExecDuration *prometheus.HistogramVec
	// 任务执行成功总数
	taskExecSuccess *prometheus.CounterVec
	// 任务执行失败总数
	taskExecFailed *prometheus.CounterVec
	// 任务重试总数
	taskExecRetry *prometheus.CounterVec
	// 任务触发总数
	taskTriggerTotal *prometheus.CounterVec
	// 任务队列大小
	taskQueueSize prometheus.GaugeFunc
}

// NewReporter 创建监控上报器
func NewReporter() *Reporter {
	r := &Reporter{
		taskExecTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: TaskExecTotal,
				Help: "Total number of task executions",
			},
			[]string{LabelTaskID, LabelStatus},
		),

		taskExecDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    TaskExecDuration,
				Help:    "Task execution duration in seconds",
				Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
			},
			[]string{LabelTaskID},
		),

		taskExecSuccess: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: TaskExecSuccess,
				Help: "Total number of successful task executions",
			},
			[]string{LabelTaskID},
		),

		taskExecFailed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: TaskExecFailed,
				Help: "Total number of failed task executions",
			},
			[]string{LabelTaskID},
		),

		taskExecRetry: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: TaskExecRetry,
				Help: "Total number of task retries",
			},
			[]string{LabelTaskID},
		),

		taskTriggerTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: TaskTriggerTotal,
				Help: "Total number of task triggers",
			},
			[]string{LabelTaskID},
		),
	}

	return r
}

// ReportExecRecord 上报任务执行记录
func (r *Reporter) ReportExecRecord(taskID string, status string) {
	r.taskExecTotal.WithLabelValues(taskID, status).Inc()
}

// ReportExecDuration 上报任务执行延迟
func (r *Reporter) ReportExecDuration(taskID string, durationSeconds float64) {
	r.taskExecDuration.WithLabelValues(taskID).Observe(durationSeconds)
}

// ReportExecSuccess 上报任务执行成功
func (r *Reporter) ReportExecSuccess(taskID string) {
	r.taskExecSuccess.WithLabelValues(taskID).Inc()
}

// ReportExecFailed 上报任务执行失败
func (r *Reporter) ReportExecFailed(taskID string) {
	r.taskExecFailed.WithLabelValues(taskID).Inc()
}

// ReportExecRetry 上报任务重试
func (r *Reporter) ReportExecRetry(taskID string) {
	r.taskExecRetry.WithLabelValues(taskID).Inc()
}

// ReportTrigger 上报任务触发
func (r *Reporter) ReportTrigger(taskID string) {
	r.taskTriggerTotal.WithLabelValues(taskID).Inc()
}

// GetHandler 获取 Prometheus HTTP handler
func GetHandler() http.Handler {
	return promhttp.Handler()
}
