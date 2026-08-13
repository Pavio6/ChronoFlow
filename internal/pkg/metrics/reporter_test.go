package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// TestReporterExportsMVPMetricsWithoutHighCardinalityLabels 验证对应的测试场景
func TestReporterExportsMVPMetricsWithoutHighCardinalityLabels(t *testing.T) {
	reporter := NewReporter()
	reporter.ReportSchedulerBatch(1, 1, 0, 50*time.Millisecond, true)
	reporter.ReportOutboxPublish(true)
	reporter.ReportWorkerExecution(ResultSuccess, 100*time.Millisecond)
	reporter.ReportWorkerRetry()
	reporter.ReportRecoveryAction("reenqueue", 1)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	byName := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		byName[family.GetName()] = family
	}
	for _, name := range []string{
		SchedulerBatchesTotal,
		SchedulerExecutions,
		OutboxPublishTotal,
		WorkerExecutionsTotal,
		WorkerDurationSeconds,
		WorkerRetriesTotal,
		RecoveryActionsTotal,
		Executions,
	} {
		if byName[name] == nil {
			t.Errorf("metric %q was not exported", name)
		}
	}
	for _, obsoleteName := range []string{
		"chronoflow_timer_exec_total",
		"chronoflow_timer_exec_duration_ms",
		"chronoflow_timer_queue_size",
		"chronoflow_records",
		"chronoflow_callback_requests_total",
		"chronoflow_redis_queue_items",
	} {
		if byName[obsoleteName] != nil {
			t.Errorf("obsolete metric %q is still exported", obsoleteName)
		}
	}

	assertOnlyLabel(t, byName[WorkerExecutionsTotal], LabelResult)
	assertOnlyLabel(t, byName[Executions], LabelStatus)
}

// assertOnlyLabel 为测试替身或测试辅助代码提供所需行为
func assertOnlyLabel(t *testing.T, family *dto.MetricFamily, label string) {
	t.Helper()
	if family == nil {
		return
	}
	for _, metric := range family.GetMetric() {
		for _, pair := range metric.GetLabel() {
			if pair.GetName() != label {
				t.Errorf("metric %q has unexpected label %q", family.GetName(), pair.GetName())
			}
		}
	}
}
