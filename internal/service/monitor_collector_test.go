package service

import (
	"context"
	"testing"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/metrics"
	redisqueue "github.com/chronoflow/internal/pkg/redis"
	"github.com/prometheus/client_golang/prometheus"
)

type stubMonitorRecordRepo struct {
	pendingBefore time.Time
	runningBefore time.Time
}

func (r *stubMonitorRecordRepo) CountByStatus() (map[model.RecordStatus]int64, error) {
	return map[model.RecordStatus]int64{
		model.RecordStatusPending: 5,
		model.RecordStatusRunning: 4,
		model.RecordStatusSuccess: 3,
		model.RecordStatusFailed:  2,
	}, nil
}

func (r *stubMonitorRecordRepo) CountPendingOverdue(before time.Time) (int64, error) {
	r.pendingBefore = before
	return 7, nil
}

func (r *stubMonitorRecordRepo) CountRunningStale(before time.Time) (int64, error) {
	r.runningBefore = before
	return 8, nil
}

type stubMonitorQueue struct{}

func (stubMonitorQueue) Stats(context.Context) (redisqueue.QueueStats, error) {
	return redisqueue.QueueStats{QueueItems: 9}, nil
}

func TestMonitorCollectorPublishesCurrentStateGauges(t *testing.T) {
	repo := &stubMonitorRecordRepo{}
	reporter := metrics.NewReporter()
	collector := NewMonitorCollector(repo, stubMonitorQueue{}, reporter, &config.MonitoringConfig{
		CollectIntervalSeconds: 10,
		PendingOverdueSeconds:  120,
		RunningStaleSeconds:    60,
	})

	collector.collect(context.Background())

	assertGauge(t, metrics.PendingOverdueRecords, nil, 7)
	assertGauge(t, metrics.RunningStaleRecords, nil, 8)
	assertGauge(t, metrics.RedisQueueItems, nil, 9)
	assertGauge(t, metrics.Records, map[string]string{metrics.LabelStatus: string(model.RecordStatusPending)}, 5)
	assertCutoff(t, repo.pendingBefore, 120*time.Second)
	assertCutoff(t, repo.runningBefore, 60*time.Second)
}

func assertCutoff(t *testing.T, cutoff time.Time, want time.Duration) {
	t.Helper()
	elapsed := time.Since(cutoff)
	if elapsed < want || elapsed > want+time.Second {
		t.Errorf("cutoff age = %s, want approximately %s", elapsed, want)
	}
}

func assertGauge(t *testing.T, name string, labels map[string]string, want float64) {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			matches := true
			for key, value := range labels {
				found := false
				for _, pair := range metric.GetLabel() {
					if pair.GetName() == key && pair.GetValue() == value {
						found = true
					}
				}
				if !found {
					matches = false
				}
			}
			if matches && metric.GetGauge().GetValue() == want {
				return
			}
		}
	}
	t.Errorf("gauge %q with labels %v did not have value %v", name, labels, want)
}
