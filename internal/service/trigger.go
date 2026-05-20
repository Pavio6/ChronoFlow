package service

import (
	"context"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/pkg/logger"
	"go.uber.org/zap"
)

// Trigger 任务触发器
// 负责从 Redis 队列中取出到期任务，并创建执行记录交给执行器处理
type Trigger struct {
	taskRepo   repository.TaskRepository
	execRepo   repository.ExecutionRepository
	redisQueue *redis.RedisQueue
	executor   *Executor
	config     *config.SchedulerConfig
	stopCh     chan struct{}
}

// NewTrigger 创建触发器实例
func NewTrigger(
	taskRepo repository.TaskRepository,
	execRepo repository.ExecutionRepository,
	redisQueue *redis.RedisQueue,
	executor *Executor,
	config *config.SchedulerConfig,
) *Trigger {
	return &Trigger{
		taskRepo:   taskRepo,
		execRepo:   execRepo,
		redisQueue: redisQueue,
		executor:   executor,
		config:     config,
		stopCh:     make(chan struct{}),
	}
}

// Start 启动触发器
func (t *Trigger) Start(ctx context.Context) {
	logger.Info("trigger started")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("trigger stopped by context")
			return
		case <-t.stopCh:
			logger.Info("trigger stopped")
			return
		case <-ticker.C:
			t.poll(ctx)
		}
	}
}

// Stop 停止触发器
func (t *Trigger) Stop() {
	close(t.stopCh)
}

// poll 从 Redis 队列中拉取到期任务
func (t *Trigger) poll(ctx context.Context) {
	// 取出到期任务
	triggers, err := t.redisQueue.PopDueTasks(ctx, int64(t.config.BatchSize))
	if err != nil {
		logger.Error("failed to pop due tasks", zap.Error(err))
		return
	}

	if len(triggers) == 0 {
		return
	}

	logger.Debug("polled due tasks", zap.Int("count", len(triggers)))

	for _, trigger := range triggers {
		t.processTrigger(ctx, trigger)
	}
}

// processTask 处理单个任务触发
func (t *Trigger) processTrigger(ctx context.Context, trigger *redis.TaskTrigger) {
	// 检查幂等性（防止重复执行）
	isIdempotent, err := t.redisQueue.IsIdempotent(ctx, trigger.TaskID, trigger.TriggerTime)
	if err != nil {
		logger.Error("failed to check idempotent",
			zap.Int64("task_id", trigger.TaskID),
			zap.Error(err),
		)
		return
	}
	if isIdempotent {
		logger.Debug("task already executed, skipping",
			zap.Int64("task_id", trigger.TaskID),
			zap.Time("trigger_time", trigger.TriggerTime),
		)
		return
	}

	// 获取分布式锁
	locked, err := t.redisQueue.AcquireTaskLock(ctx, trigger.TaskID, trigger.TriggerTime, 5*time.Minute)
	if err != nil {
		logger.Error("failed to acquire task lock",
			zap.Int64("task_id", trigger.TaskID),
			zap.Error(err),
		)
		return
	}
	if !locked {
		logger.Debug("task lock acquired by another instance",
			zap.Int64("task_id", trigger.TaskID),
		)
		return
	}

	// 设置幂等键
	set, err := t.redisQueue.SetIdempotentKey(ctx, trigger.TaskID, trigger.TriggerTime, 24*time.Hour)
	if err != nil {
		logger.Error("failed to set idempotent key",
			zap.Int64("task_id", trigger.TaskID),
			zap.Error(err),
		)
		t.redisQueue.ReleaseTaskLock(ctx, trigger.TaskID, trigger.TriggerTime)
		return
	}
	if !set {
		logger.Debug("idempotent key already set, skipping",
			zap.Int64("task_id", trigger.TaskID),
		)
		t.redisQueue.ReleaseTaskLock(ctx, trigger.TaskID, trigger.TriggerTime)
		return
	}

	// 获取任务详情
	task, err := t.taskRepo.GetByID(trigger.TaskID)
	if err != nil {
		logger.Error("failed to get task",
			zap.Int64("task_id", trigger.TaskID),
			zap.Error(err),
		)
		t.redisQueue.ReleaseTaskLock(ctx, trigger.TaskID, trigger.TriggerTime)
		return
	}

	// 检查任务状态
	if !task.IsRunnable() {
		logger.Debug("task is not runnable, skipping",
			zap.Int64("task_id", trigger.TaskID),
			zap.String("status", string(task.Status)),
		)
		t.redisQueue.ReleaseTaskLock(ctx, trigger.TaskID, trigger.TriggerTime)
		return
	}

	// 创建执行记录
	execution := &model.TaskExecution{
		TaskID:        task.ID,
		TriggerTime:   trigger.TriggerTime,
		Status:        model.ExecutionStatusPENDING,
		RequestURL:    task.CallbackURL,
		RequestMethod: task.CallbackMethod,
		RequestBody:   task.CallbackBody,
	}

	if err := t.execRepo.Create(execution); err != nil {
		logger.Error("failed to create execution",
			zap.Int64("task_id", trigger.TaskID),
			zap.Error(err),
		)
		t.redisQueue.ReleaseTaskLock(ctx, trigger.TaskID, trigger.TriggerTime)
		return
	}

	// 释放锁
	t.redisQueue.ReleaseTaskLock(ctx, trigger.TaskID, trigger.TriggerTime)

	// 提交到执行器异步执行
	t.executor.Submit(execution, task)

	logger.Info("task triggered",
		zap.Int64("task_id", task.ID),
		zap.Int64("execution_id", execution.ID),
		zap.Time("trigger_time", trigger.TriggerTime),
	)
}
