package service

import (
	"context"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/logger"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/repository"
	"go.uber.org/zap"
)

// Reconciler 独立于 Redis 可用性修复持久化 Execution
type Reconciler struct {
	recoveryRepo  repository.RecoveryRepository
	executionRepo repository.TimerExecutionRepository
	reporter      *metrics.Reporter
	cfg           config.RecoveryConfig
	now           func() time.Time
}

// NewReconciler 创建负责执行恢复、清理和指标采集的 Reconciler
func NewReconciler(
	recoveryRepo repository.RecoveryRepository,
	executionRepo repository.TimerExecutionRepository,
	reporter *metrics.Reporter,
	cfg *config.RecoveryConfig,
) *Reconciler {
	return &Reconciler{
		recoveryRepo:  recoveryRepo,
		executionRepo: executionRepo,
		reporter:      reporter,
		cfg:           *cfg,
		now:           time.Now,
	}
}

// Start 按配置间隔执行恢复、清理和 Execution 指标采集
func (r *Reconciler) Start(ctx context.Context) {
	if !r.cfg.Enabled {
		return
	}
	logger.Info("Execution reconciler started",
		zap.Int("scan_interval_seconds", r.cfg.ScanIntervalSeconds),
	)
	r.reconcile(ctx)
	scanTicker := time.NewTicker(
		time.Duration(r.cfg.ScanIntervalSeconds) * time.Second,
	)
	cleanupTicker := time.NewTicker(
		time.Duration(r.cfg.CleanupIntervalMinutes) * time.Minute,
	)
	defer scanTicker.Stop()
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Execution reconciler stopped")
			return
		case <-scanTicker.C:
			r.reconcile(ctx)
		case <-cleanupTicker.C:
			r.cleanup(ctx)
		}
	}
}

// reconcile 扫描并恢复停滞、Lease 过期或需要重新投递的 Execution
func (r *Reconciler) reconcile(ctx context.Context) {
	now := r.now()
	result, err := r.recoveryRepo.RecoverBatch(
		ctx,
		now,
		now.Add(-time.Duration(r.cfg.PendingStaleSeconds)*time.Second),
		r.cfg.BatchSize,
	)
	if err != nil {
		r.reporter.ReportRecoveryFailure()
		logger.Error("Reconciler recovery scan failed", zap.Error(err))
		return
	}
	r.reporter.ReportRecoveryAction("reenqueue", result.Reenqueued)
	r.reporter.ReportRecoveryAction("expired_lease", result.ExpiredLeases)
	r.reporter.ReportRecoveryAction("terminal_failure", result.FinalFailures)
	if result.Reenqueued > 0 || result.ExpiredLeases > 0 {
		logger.Warn("Reconciler repaired execution state",
			zap.Int("reenqueued", result.Reenqueued),
			zap.Int("expired_leases", result.ExpiredLeases),
			zap.Int("final_failures", result.FinalFailures),
		)
	}
	r.collectExecutionMetrics(ctx)
}

// cleanup 清理超过保留期限的 Outbox 事件和终态 Execution
func (r *Reconciler) cleanup(ctx context.Context) {
	now := r.now()
	result, err := r.recoveryRepo.Cleanup(
		ctx,
		now.Add(-time.Duration(r.cfg.OutboxRetentionDays)*24*time.Hour),
		now.Add(-time.Duration(r.cfg.ExecutionRetentionDays)*24*time.Hour),
		r.cfg.BatchSize,
	)
	if err != nil {
		r.reporter.ReportRecoveryFailure()
		logger.Error("Reconciler history cleanup failed", zap.Error(err))
		return
	}
	r.reporter.ReportRecoveryAction(
		"cleanup",
		int(result.OutboxEvents+result.Executions),
	)
}

// collectExecutionMetrics 统计并上报各 Execution 状态的数量
func (r *Reconciler) collectExecutionMetrics(ctx context.Context) {
	counts, err := r.executionRepo.CountByStatus(ctx)
	if err != nil {
		r.reporter.ReportRecoveryFailure()
		return
	}
	for _, status := range []model.ExecutionStatus{
		model.ExecutionStatusPending,
		model.ExecutionStatusRunning,
		model.ExecutionStatusRetryWait,
		model.ExecutionStatusSuccess,
		model.ExecutionStatusFailed,
		model.ExecutionStatusCancelled,
	} {
		r.reporter.SetExecutionCount(string(status), counts[status])
	}
}

type streamRetention interface {
	TrimAcknowledgedBefore(
		context.Context,
		string,
		string,
		time.Time,
	) (int64, error)
}

// StreamRetentionCleaner 仅删除已确认的历史消息，并保留最早 Pending 消息边界
type StreamRetentionCleaner struct {
	stream   streamRetention
	reporter *metrics.Reporter
	outbox   config.OutboxConfig
	recovery config.RecoveryConfig
	now      func() time.Time
}

// NewStreamRetentionCleaner 创建 Redis Stream 保留数据清理器
func NewStreamRetentionCleaner(
	stream streamRetention,
	reporter *metrics.Reporter,
	outbox *config.OutboxConfig,
	recovery *config.RecoveryConfig,
) *StreamRetentionCleaner {
	return &StreamRetentionCleaner{
		stream:   stream,
		reporter: reporter,
		outbox:   *outbox,
		recovery: *recovery,
		now:      time.Now,
	}
}

// Start 按配置间隔清理超过保留期限的已确认 Stream 消息
func (c *StreamRetentionCleaner) Start(ctx context.Context) {
	if !c.recovery.Enabled {
		return
	}
	ticker := time.NewTicker(
		time.Duration(c.recovery.CleanupIntervalMinutes) * time.Minute,
	)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.cleanup(ctx)
		}
	}
}

// cleanup 执行一次已确认 Stream 消息清理并上报结果
func (c *StreamRetentionCleaner) cleanup(ctx context.Context) {
	trimmed, err := c.stream.TrimAcknowledgedBefore(
		ctx,
		c.outbox.Stream,
		c.outbox.ConsumerGroup,
		c.now().Add(
			-time.Duration(c.recovery.StreamRetentionHours)*time.Hour,
		),
	)
	if err != nil {
		c.reporter.ReportRecoveryFailure()
		logger.Error("Failed to clean Redis Stream history", zap.Error(err))
		return
	}
	c.reporter.ReportRecoveryAction("cleanup", int(trimmed))
}
