package metrics

import (
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Prometheus 指标名称常量
const (
	// TimerExecTotal 定时器执行总次数
	TimerExecTotal = "chronoflow_timer_exec_total"
	// TimerExecDuration 定时器执行耗时（直方图，毫秒）
	TimerExecDuration = "chronoflow_timer_exec_duration_ms"
	// TimerExecSuccess 定时器执行成功次数
	TimerExecSuccess = "chronoflow_timer_exec_success_total"
	// TimerExecFailed 定时器执行失败次数
	TimerExecFailed = "chronoflow_timer_exec_failed_total"
	// TimerTriggerTotal 定时器触发总次数
	TimerTriggerTotal = "chronoflow_timer_trigger_total"
	// TimerQueueSize 定时器队列当前大小
	TimerQueueSize = "chronoflow_timer_queue_size"
)

// 标签常量
const (
	// LabelTimerID 定时器 ID 标签
	LabelTimerID = "timer_id"
	// LabelStatus 执行状态标签
	LabelStatus = "status"
	// LabelApp 应用名称标签
	LabelApp = "app"
)

// Reporter Prometheus 指标上报器（单例）
type Reporter struct {
	// execTotal 定时器执行总次数
	execTotal *prometheus.CounterVec
	// execDuration 定时器执行耗时
	execDuration *prometheus.HistogramVec
	// execSuccess 定时器执行成功次数
	execSuccess *prometheus.CounterVec
	// execFailed 定时器执行失败次数
	execFailed *prometheus.CounterVec
	// triggerTotal 定时器触发总次数
	triggerTotal *prometheus.CounterVec
	// queueSize 定时器队列大小
	queueSize         prometheus.GaugeFunc
	execTotalCount    atomic.Int64
	execSuccessCount  atomic.Int64
	execFailedCount   atomic.Int64
	triggerTotalCount atomic.Int64
	durationTotalMs   atomic.Int64
	durationCount     atomic.Int64
}

// Snapshot 当前进程内的指标快照，供前端监控页使用
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
	// 单例实例和初始化保障
	reporterInstance *Reporter
	reporterOnce     sync.Once
)

// NewReporter 创建指标上报器单例
// 使用 sync.Once 保证只注册一次 Prometheus 指标
func NewReporter() *Reporter {
	reporterOnce.Do(func() {
		reporterInstance = &Reporter{
			execTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: TimerExecTotal,
				Help: "定时器执行总次数",
			}, []string{LabelTimerID, LabelApp}),

			execDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
				Name:    TimerExecDuration,
				Help:    "定时器执行耗时（毫秒）",
				Buckets: []float64{10, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
			}, []string{LabelTimerID, LabelApp}),

			execSuccess: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: TimerExecSuccess,
				Help: "定时器执行成功次数",
			}, []string{LabelTimerID, LabelApp}),

			execFailed: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: TimerExecFailed,
				Help: "定时器执行失败次数",
			}, []string{LabelTimerID, LabelApp, LabelStatus}),

			triggerTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: TimerTriggerTotal,
				Help: "定时器触发总次数",
			}, []string{LabelApp}),

			queueSize: promauto.NewGaugeFunc(prometheus.GaugeOpts{
				Name: TimerQueueSize,
				Help: "定时器队列当前大小",
			}, func() float64 {
				// 默认返回 0，由外部注入实际值
				return 0
			}),
		}
	})

	return reporterInstance
}

// ReportExecRecord 上报定时器执行记录
func (r *Reporter) ReportExecRecord(timerID int64, app string) {
	r.execTotal.With(prometheus.Labels{
		LabelTimerID: formatID(timerID),
		LabelApp:     app,
	}).Inc()
	r.execTotalCount.Add(1)
}

// ReportExecDuration 上报定时器执行耗时
func (r *Reporter) ReportExecDuration(timerID int64, app string, durationMs float64) {
	r.execDuration.With(prometheus.Labels{
		LabelTimerID: formatID(timerID),
		LabelApp:     app,
	}).Observe(durationMs)
	r.durationTotalMs.Add(int64(durationMs))
	r.durationCount.Add(1)
}

// ReportExecSuccess 上报定时器执行成功
func (r *Reporter) ReportExecSuccess(timerID int64, app string) {
	r.execSuccess.With(prometheus.Labels{
		LabelTimerID: formatID(timerID),
		LabelApp:     app,
	}).Inc()
	r.execSuccessCount.Add(1)
}

// ReportExecFailed 上报定时器执行失败
func (r *Reporter) ReportExecFailed(timerID int64, app, status string) {
	r.execFailed.With(prometheus.Labels{
		LabelTimerID: formatID(timerID),
		LabelApp:     app,
		LabelStatus:  status,
	}).Inc()
	r.execFailedCount.Add(1)
}

// ReportTrigger 上报定时器触发
func (r *Reporter) ReportTrigger(app string) {
	r.triggerTotal.With(prometheus.Labels{
		LabelApp: app,
	}).Inc()
	r.triggerTotalCount.Add(1)
}

// Snapshot 返回当前进程内的聚合指标
func (r *Reporter) Snapshot() Snapshot {
	execTotal := r.execTotalCount.Load()
	durationCount := r.durationCount.Load()
	success := r.execSuccessCount.Load()

	var avgDuration float64
	if durationCount > 0 {
		avgDuration = float64(r.durationTotalMs.Load()) / float64(durationCount)
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
		LastCollectedMsg: "process-local",
	}
}

// GetHandler 返回 Prometheus HTTP 指标暴露 handler
func (r *Reporter) GetHandler() http.Handler {
	return promhttp.Handler()
}

// formatID 将 int64 ID 格式化为字符串
func formatID(id int64) string {
	return strconv.FormatInt(id, 10)
}
