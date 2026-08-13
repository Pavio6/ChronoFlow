package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/logger"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/repository"
	"go.uber.org/zap"
)

type outboxStreamPublisher interface {
	Publish(
		ctx context.Context,
		stream string,
		maxLen int64,
		event *model.OutboxEvent,
	) (string, error)
}

// OutboxDispatcher 将已提交的 MySQL 事件发布到 Redis Stream
// 若在 XADD 后、MarkPublished 前崩溃，后续恢复会产生可预期的重复消息
type OutboxDispatcher struct {
	repo      repository.OutboxRepository
	publisher outboxStreamPublisher
	reporter  *metrics.Reporter
	cfg       config.OutboxConfig
	owner     string
	now       func() time.Time
}

// NewOutboxDispatcher 创建负责将 Outbox 事件投递到 Redis Stream 的 Dispatcher
func NewOutboxDispatcher(
	repo repository.OutboxRepository,
	publisher outboxStreamPublisher,
	reporter *metrics.Reporter,
	cfg *config.OutboxConfig,
	owner string,
) *OutboxDispatcher {
	return &OutboxDispatcher{
		repo:      repo,
		publisher: publisher,
		reporter:  reporter,
		cfg:       *cfg,
		owner:     owner,
		now:       time.Now,
	}
}

// Start 按配置的轮询间隔持续投递待发布的 Outbox 事件
func (d *OutboxDispatcher) Start(ctx context.Context) {
	logger.Info("Outbox dispatcher started",
		zap.String("owner", d.owner),
		zap.String("stream", d.cfg.Stream),
		zap.Int("batch_size", d.cfg.BatchSize),
	)

	d.dispatchOnce(ctx)
	ticker := time.NewTicker(time.Duration(d.cfg.PollIntervalMS) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("Outbox dispatcher stopped", zap.String("owner", d.owner))
			return
		case <-ticker.C:
			d.dispatchOnce(ctx)
		}
	}
}

// dispatchOnce 领取一批 Outbox 事件，并分别记录发布成功或失败结果
func (d *OutboxDispatcher) dispatchOnce(ctx context.Context) {
	now := d.now()
	events, err := d.repo.ClaimBatch(
		ctx,
		d.owner,
		now,
		d.cfg.BatchSize,
		time.Duration(d.cfg.ClaimTTLSeconds)*time.Second,
	)
	if err != nil {
		logger.Error("Outbox dispatcher failed to claim events", zap.Error(err))
		d.refreshBacklogMetric(ctx)
		return
	}

	for _, event := range events {
		if ctx.Err() != nil {
			return
		}
		messageID, publishErr := d.publisher.Publish(
			ctx,
			d.cfg.Stream,
			d.cfg.StreamMaxLen,
			event,
		)
		if publishErr != nil {
			nextAttempt := now.Add(d.backoff(event.Attempts + 1))
			markErr := d.repo.MarkFailed(
				ctx,
				event.EventID,
				d.owner,
				truncateOutboxError(publishErr.Error()),
				nextAttempt,
			)
			if markErr != nil {
				logger.Error("Outbox dispatcher failed to record publish failure",
					zap.String("event_id", event.EventID),
					zap.Error(markErr),
				)
			}
			d.reporter.ReportOutboxPublish(false)
			logger.Warn("Outbox dispatcher failed to publish to Redis Stream",
				zap.String("event_id", event.EventID),
				zap.Time("next_attempt_at", nextAttempt),
				zap.Error(publishErr),
			)
			continue
		}

		if err := d.repo.MarkPublished(
			ctx,
			event.EventID,
			d.owner,
			messageID,
			d.now(),
		); err != nil {
			// Redis 消息已写入。等待领取 Lease 过期后事件会再次发布，
			// 后续 Worker 通过 MySQL Execution Claim 完成去重
			d.reporter.ReportOutboxPublish(false)
			logger.Error("Outbox dispatcher failed to confirm publication; event may be delivered again",
				zap.String("event_id", event.EventID),
				zap.String("message_id", messageID),
				zap.Error(err),
			)
			continue
		}
		d.reporter.ReportOutboxPublish(true)
	}

	d.refreshBacklogMetric(ctx)
}

// refreshBacklogMetric 刷新未发布 Outbox 事件数量指标
func (d *OutboxDispatcher) refreshBacklogMetric(ctx context.Context) {
	count, err := d.repo.CountUnpublished(ctx)
	if err != nil {
		logger.Error("Outbox dispatcher failed to count backlog", zap.Error(err))
		return
	}
	d.reporter.SetOutboxUnpublished(count)
}

// backoff 根据尝试次数计算受上限限制的指数退避时间
func (d *OutboxDispatcher) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	exponent := attempt - 1
	if exponent > 30 {
		exponent = 30
	}
	delaySeconds := int64(1) << exponent
	maxSeconds := int64(d.cfg.MaxBackoffSeconds)
	if delaySeconds > maxSeconds {
		delaySeconds = maxSeconds
	}
	return time.Duration(delaySeconds) * time.Second
}

// truncateOutboxError 清理并截断准备持久化的 Outbox 错误信息
func truncateOutboxError(message string) string {
	const maxLength = 2048
	message = strings.TrimSpace(message)
	if len(message) <= maxLength {
		return message
	}
	return fmt.Sprintf("%s...", message[:maxLength-3])
}
