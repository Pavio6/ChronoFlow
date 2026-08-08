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

// OutboxDispatcher publishes committed MySQL events to Redis Streams. A crash
// after XADD and before MarkPublished intentionally produces a duplicate.
type OutboxDispatcher struct {
	repo      repository.OutboxRepository
	publisher outboxStreamPublisher
	reporter  *metrics.Reporter
	cfg       config.OutboxConfig
	owner     string
	now       func() time.Time
}

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
			// The Redis message already exists. Leaving the claim to expire makes
			// the event publish again, and the future Worker must deduplicate via
			// its MySQL execution claim.
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

func (d *OutboxDispatcher) refreshBacklogMetric(ctx context.Context) {
	count, err := d.repo.CountUnpublished(ctx)
	if err != nil {
		logger.Error("Outbox dispatcher failed to count backlog", zap.Error(err))
		return
	}
	d.reporter.SetOutboxUnpublished(count)
}

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

func truncateOutboxError(message string) string {
	const maxLength = 2048
	message = strings.TrimSpace(message)
	if len(message) <= maxLength {
		return message
	}
	return fmt.Sprintf("%s...", message[:maxLength-3])
}
