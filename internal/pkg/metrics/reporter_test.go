package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestReporterExportsMVPMetricsWithoutHighCardinalityLabels(t *testing.T) {
	reporter := NewReporter()
	reporter.ReportCallback(ResultSuccess, 250*time.Millisecond)
	reporter.SetRecordCount("PENDING", 2)
	reporter.SetPendingOverdueRecords(1)
	reporter.SetRunningStaleRecords(3)
	reporter.SetRedisQueueItems(4)
	reporter.SetRecordSuccessRate(94.5)
	reporter.SetRecordDurationP95Milliseconds(315)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	byName := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		byName[family.GetName()] = family
	}
	for _, name := range []string{
		CallbackRequestsTotal,
		CallbackDurationSeconds,
		Records,
		PendingOverdueRecords,
		RunningStaleRecords,
		RedisQueueItems,
		RecordSuccessRate,
		RecordDurationP95Ms,
	} {
		if byName[name] == nil {
			t.Errorf("metric %q was not exported", name)
		}
	}
	for _, oldName := range []string{
		"chronoflow_timer_exec_total",
		"chronoflow_timer_exec_duration_ms",
		"chronoflow_timer_queue_size",
	} {
		if byName[oldName] != nil {
			t.Errorf("legacy metric %q is still exported", oldName)
		}
	}

	assertOnlyLabel(t, byName[CallbackRequestsTotal], LabelResult)
	assertOnlyLabel(t, byName[CallbackDurationSeconds], LabelResult)
	assertOnlyLabel(t, byName[Records], LabelStatus)
}

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
