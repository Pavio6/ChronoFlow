package service

import (
	"context"
	"fmt"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/cron"
	"github.com/chronoflow/internal/pkg/logger"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/repository"
	"go.uber.org/zap"
)

// Scheduler turns due timer definitions into durable executions and Outbox
// events. Redis is intentionally absent from this component.
type Scheduler struct {
	repo     repository.DueTimerRepository
	parser   *cron.CronParser
	reporter *metrics.Reporter
	cfg      config.SchedulerConfig
	now      func() time.Time
}

func NewScheduler(
	repo repository.DueTimerRepository,
	parser *cron.CronParser,
	reporter *metrics.Reporter,
	cfg *config.SchedulerConfig,
) *Scheduler {
	return &Scheduler{
		repo:     repo,
		parser:   parser,
		reporter: reporter,
		cfg:      *cfg,
		now:      time.Now,
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	logger.Info("Scheduler started",
		zap.Int("poll_interval_ms", s.cfg.PollIntervalMS),
		zap.Int("batch_size", s.cfg.BatchSize),
	)

	s.schedule(ctx)
	ticker := time.NewTicker(time.Duration(s.cfg.PollIntervalMS) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("Scheduler stopped")
			return
		case <-ticker.C:
			s.schedule(ctx)
		}
	}
}

func (s *Scheduler) schedule(ctx context.Context) {
	start := time.Now()
	now := s.now()
	result, err := s.repo.ScheduleDueBatch(ctx, now, s.cfg.BatchSize, s.resolveTimer)
	if err != nil {
		logger.Error("Scheduler batch failed", zap.Error(err))
		s.reporter.ReportSchedulerBatch(0, 0, 0, time.Since(start), false)
		return
	}
	s.reporter.ReportSchedulerBatch(
		result.Timers,
		result.Executions,
		result.Duplicates,
		time.Since(start),
		true,
	)
	if result.Timers > 0 {
		logger.Info("Scheduler batch completed",
			zap.Int("timers", result.Timers),
			zap.Int("executions", result.Executions),
			zap.Int("duplicates", result.Duplicates),
			zap.Duration("elapsed", time.Since(start)),
		)
	}
}

func (s *Scheduler) resolveTimer(
	definition *model.TimerDefinition,
	now time.Time,
) ([]time.Time, time.Time, error) {
	if definition.NextFireAt == nil {
		return nil, time.Time{}, fmt.Errorf("ACTIVE timer has an empty next_fire_at")
	}
	timezone := definition.Timezone
	if timezone == "" {
		timezone = time.Local.String()
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("load timezone %q: %w", timezone, err)
	}

	current := definition.NextFireAt.In(location)
	nowLocal := now.In(location)
	nextAfter := func(from time.Time) (time.Time, error) {
		next, err := s.parser.NextTriggerTime(definition.CronExpr, from)
		if err != nil {
			return time.Time{}, err
		}
		return next.In(location), nil
	}

	grace := time.Duration(s.cfg.MisfireGraceSeconds) * time.Second
	overdue := now.Sub(*definition.NextFireAt) > grace
	policy := definition.MisfirePolicy
	if policy == "" {
		policy = model.MisfirePolicyFireOnce
	}

	if !overdue && policy != model.MisfirePolicyCatchUp {
		next, err := nextAfter(current)
		if err != nil {
			return nil, time.Time{}, err
		}
		return []time.Time{current.Local()}, next.Local(), nil
	}

	switch policy {
	case model.MisfirePolicySkip:
		next, err := nextAfter(nowLocal)
		if err != nil {
			return nil, time.Time{}, err
		}
		return nil, next.Local(), nil
	case model.MisfirePolicyFireOnce:
		next, err := nextAfter(nowLocal)
		if err != nil {
			return nil, time.Time{}, err
		}
		return []time.Time{current.Local()}, next.Local(), nil
	case model.MisfirePolicyCatchUp:
		limit := definition.MaxCatchUp
		if limit < 1 {
			limit = s.cfg.DefaultMaxCatchUp
		}
		occurrences := make([]time.Time, 0, limit)
		cursor := current
		for !cursor.After(nowLocal) && len(occurrences) < limit {
			occurrences = append(occurrences, cursor.Local())
			cursor, err = nextAfter(cursor)
			if err != nil {
				return nil, time.Time{}, err
			}
		}
		return occurrences, cursor.Local(), nil
	default:
		return nil, time.Time{}, fmt.Errorf("unknown misfire policy: %s", policy)
	}
}
