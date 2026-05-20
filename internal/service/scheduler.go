package service

import (
	"context"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	cronpkg "github.com/chronoflow/internal/pkg/cron"
	"github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/pkg/logger"
	"go.uber.org/zap"
)

// Scheduler 任务调度器
// 负责定期扫描数据库中的任务，计算下次触发时间，并将任务推入 Redis 队列
type Scheduler struct {
	taskRepo   repository.TaskRepository
	redisQueue *redis.RedisQueue
	cronParser *cronpkg.CronParser
	config     *config.SchedulerConfig
	stopCh     chan struct{}
}

// NewScheduler 创建调度器实例
func NewScheduler(
	taskRepo repository.TaskRepository,
	redisQueue *redis.RedisQueue,
	config *config.SchedulerConfig,
) *Scheduler {
	return &Scheduler{
		taskRepo:   taskRepo,
		redisQueue: redisQueue,
		cronParser: cronpkg.NewCronParser(),
		config:     config,
		stopCh:     make(chan struct{}),
	}
}

// Start 启动调度器
func (s *Scheduler) Start(ctx context.Context) {
	logger.Info("scheduler started",
		zap.Int("scan_interval", s.config.ScanInterval),
		zap.Int("batch_size", s.config.BatchSize),
	)

	ticker := time.NewTicker(time.Duration(s.config.ScanInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("scheduler stopped by context")
			return
		case <-s.stopCh:
			logger.Info("scheduler stopped")
			return
		case <-ticker.C:
			s.scan(ctx)
		}
	}
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	close(s.stopCh)
}

// scan 扫描需要调度的任务
func (s *Scheduler) scan(ctx context.Context) {
	// 获取需要调度的任务
	tasks, err := s.taskRepo.GetTasksToSchedule(s.config.BatchSize)
	if err != nil {
		logger.Error("failed to get tasks to schedule", zap.Error(err))
		return
	}

	if len(tasks) == 0 {
		return
	}

	logger.Debug("scanning tasks", zap.Int("count", len(tasks)))

	for _, task := range tasks {
		s.scheduleTask(ctx, task)
	}
}

// scheduleTask 调度单个任务
func (s *Scheduler) scheduleTask(ctx context.Context, task *model.Task) {
	// 计算下次触发时间
	nextTime, err := s.cronParser.NextTriggerTime(task.CronExpr, time.Now())
	if err != nil {
		logger.Error("failed to calculate next trigger time",
			zap.Int64("task_id", task.ID),
			zap.String("cron_expr", task.CronExpr),
			zap.Error(err),
		)
		return
	}

	// 更新数据库中的下次触发时间
	if err := s.taskRepo.UpdateNextTriggerTime(task.ID, &nextTime); err != nil {
		logger.Error("failed to update next trigger time",
			zap.Int64("task_id", task.ID),
			zap.Error(err),
		)
		return
	}

	// 推入 Redis 队列
	trigger := &redis.TaskTrigger{
		TaskID:      task.ID,
		TriggerTime: nextTime,
	}

	if err := s.redisQueue.PushTask(ctx, trigger); err != nil {
		logger.Error("failed to push task to queue",
			zap.Int64("task_id", task.ID),
			zap.Error(err),
		)
		return
	}

	logger.Debug("task scheduled",
		zap.Int64("task_id", task.ID),
		zap.Time("next_trigger_time", nextTime),
	)
}
