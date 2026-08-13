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

// Scheduler 将到期 Timer 转换为持久化 Execution 和 Outbox 事件，不直接依赖 Redis
type Scheduler struct {
	repo     repository.DueTimerRepository
	parser   *cron.CronParser
	reporter *metrics.Reporter
	cfg      config.SchedulerConfig
	now      func() time.Time
}

// NewScheduler 创建以 MySQL 为权威数据源的定时调度器
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

// Start 按配置的轮询间隔扫描并调度到期 Timer
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

// schedule 执行一次到期 Timer 领取、Execution 创建与指标上报
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

// resolveTimer 根据 Cron 和错过触发策略生成本轮触发点及新的 next_fire_at
// 返回值依次为本轮需要创建 Execution 的触发点列表、推进后的下一次触发时间和计算错误
func (s *Scheduler) resolveTimer(
	definition *model.TimerDefinition,
	now time.Time,
) ([]time.Time, time.Time, error) {
	if definition.NextFireAt == nil {
		return nil, time.Time{}, fmt.Errorf("ACTIVE timer has an empty next_fire_at")
	}
	current := *definition.NextFireAt
	nextAfter := func(from time.Time) (time.Time, error) {
		return s.parser.NextTriggerTime(definition.CronExpr, from)
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
		return []time.Time{current}, next, nil
	}

	switch policy {
	case model.MisfirePolicySkip:
		next, err := nextAfter(now)
		if err != nil {
			return nil, time.Time{}, err
		}
		return nil, next, nil
	case model.MisfirePolicyFireOnce:
		next, err := nextAfter(now)
		if err != nil {
			return nil, time.Time{}, err
		}
		return []time.Time{current}, next, nil
	case model.MisfirePolicyCatchUp:
		limit := definition.MaxCatchUp
		if limit < 1 {
			limit = s.cfg.DefaultMaxCatchUp
		}
		// 预分配最多容纳 limit 个历史触发点的列表，避免补发过程中重复扩容
		occurrences := make([]time.Time, 0, limit)
		// cursor 始终指向当前尚未处理的历史触发点，初始值为原 next_fire_at
		cursor := current
		// 仅补发不晚于当前时间的触发点，并在达到上限时停止，避免恢复后任务堆积
		for !cursor.After(now) && len(occurrences) < limit {
			// 当前 cursor 是一个遗漏触发点，为它创建一条 Execution
			occurrences = append(occurrences, cursor)
			// 计算下一个历史触发点，作为下一轮循环的游标
			next, err := nextAfter(cursor)
			if err != nil {
				return nil, time.Time{}, err
			}
			cursor = next
		}
		// cursor 已推进到第一个未补发的触发点，作为新的 next_fire_at 返回
		return occurrences, cursor, nil
	default:
		return nil, time.Time{}, fmt.Errorf("unknown misfire policy: %s", policy)
	}
}
