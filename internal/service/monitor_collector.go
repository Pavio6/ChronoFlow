package service

import (
	"context"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/metrics"
	redisqueue "github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/pkg/logger"
	"go.uber.org/zap"
)

type monitorRecordRepository interface {
	CountByStatus() (map[model.RecordStatus]int64, error)
	CountPendingOverdue(before time.Time) (int64, error)
	CountRunningStale(before time.Time) (int64, error)
	CompletedDurationP95Milliseconds() (float64, error)
}

type monitorQueue interface {
	Stats(ctx context.Context) (redisqueue.QueueStats, error)
}

// MonitorCollector periodically refreshes metrics that describe current system state.
type MonitorCollector struct {
	recRepo  monitorRecordRepository
	queue    monitorQueue
	reporter *metrics.Reporter
	cfg      config.MonitoringConfig
}

func NewMonitorCollector(
	recRepo monitorRecordRepository,
	queue monitorQueue,
	reporter *metrics.Reporter,
	cfg *config.MonitoringConfig,
) *MonitorCollector {
	return &MonitorCollector{
		recRepo:  recRepo,
		queue:    queue,
		reporter: reporter,
		cfg:      *cfg,
	}
}

func (c *MonitorCollector) Start(ctx context.Context) {
	c.collect(ctx)

	ticker := time.NewTicker(time.Duration(c.cfg.CollectIntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collect(ctx)
		}
	}
}

func (c *MonitorCollector) collect(ctx context.Context) {
	statusCounts, err := c.recRepo.CountByStatus()
	if err != nil {
		logger.Error("采集执行记录状态指标失败", zap.Error(err))
	} else {
		for _, status := range []model.RecordStatus{
			model.RecordStatusPending,
			model.RecordStatusRunning,
			model.RecordStatusSuccess,
			model.RecordStatusFailed,
		} {
			c.reporter.SetRecordCount(string(status), statusCounts[status])
		}
		completed := statusCounts[model.RecordStatusSuccess] + statusCounts[model.RecordStatusFailed]
		var successRate float64
		if completed > 0 {
			successRate = 100 * float64(statusCounts[model.RecordStatusSuccess]) / float64(completed)
		}
		c.reporter.SetRecordSuccessRate(successRate)
	}

	p95, err := c.recRepo.CompletedDurationP95Milliseconds()
	if err != nil {
		logger.Error("采集已完成执行 P95 耗时指标失败", zap.Error(err))
	} else {
		c.reporter.SetRecordDurationP95Milliseconds(p95)
	}

	now := time.Now()
	pending, err := c.recRepo.CountPendingOverdue(now.Add(-time.Duration(c.cfg.PendingOverdueSeconds) * time.Second))
	if err != nil {
		logger.Error("采集超期待执行指标失败", zap.Error(err))
	} else {
		c.reporter.SetPendingOverdueRecords(pending)
	}

	running, err := c.recRepo.CountRunningStale(now.Add(-time.Duration(c.cfg.RunningStaleSeconds) * time.Second))
	if err != nil {
		logger.Error("采集卡住执行指标失败", zap.Error(err))
	} else {
		c.reporter.SetRunningStaleRecords(running)
	}

	queueStats, err := c.queue.Stats(ctx)
	if err != nil {
		logger.Error("采集 Redis 队列指标失败", zap.Error(err))
	} else {
		c.reporter.SetRedisQueueItems(queueStats.QueueItems)
	}
}
