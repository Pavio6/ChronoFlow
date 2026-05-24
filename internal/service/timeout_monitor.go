package service

import (
	"context"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/pkg/logger"
	"go.uber.org/zap"
)

// TimeoutMonitor 将长时间停留在 RUNNING 的执行记录标记为 TIMEOUT。
type TimeoutMonitor struct {
	recRepo repository.TimerRecordRepository
	cfg     *config.ExecutorConfig
	quit    chan struct{}
}

func NewTimeoutMonitor(recRepo repository.TimerRecordRepository, cfg *config.ExecutorConfig) *TimeoutMonitor {
	return &TimeoutMonitor{
		recRepo: recRepo,
		cfg:     cfg,
		quit:    make(chan struct{}),
	}
}

func (m *TimeoutMonitor) Start(ctx context.Context) {
	interval := time.Duration(m.cfg.Timeout) * time.Second / 2
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.scan()
		case <-m.quit:
			logger.Info("TimeoutMonitor 停止")
			return
		case <-ctx.Done():
			logger.Info("TimeoutMonitor 因 context 取消而停止")
			return
		}
	}
}

func (m *TimeoutMonitor) Stop() {
	close(m.quit)
}

func (m *TimeoutMonitor) scan() {
	timeout := time.Duration(m.cfg.Timeout) * time.Second
	records, err := m.recRepo.GetRunningRecords(timeout)
	if err != nil {
		logger.Error("TimeoutMonitor 查询超时执行记录失败", zap.Error(err))
		return
	}

	for _, record := range records {
		now := time.Now()
		record.Status = model.RecordStatusTimeout
		record.FinishedAt = &now
		record.ErrorMessage = "execution exceeded configured timeout"
		if record.StartedAt != nil {
			record.Duration = now.Sub(*record.StartedAt).Milliseconds()
		}
		if err := m.recRepo.Update(record); err != nil {
			logger.Error("TimeoutMonitor 标记执行记录超时失败",
				zap.Int64("record_id", record.ID),
				zap.Error(err),
			)
		}
	}
}
