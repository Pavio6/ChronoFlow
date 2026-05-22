package service

import (
	"context"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/pkg/pool"
	"github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/pkg/logger"
	"go.uber.org/zap"
)

// Trigger 触发器（对应 xTimer 的 Trigger 模块）
// 被 Scheduler 启动后，在时间片内持续轮询 Redis ZSet
// 取出到期任务后从协程池提交 Executor 协程执行
// 核心流程：
//  1. 在时间片内持续轮询 Redis ZSet {time_range}:{bucket}
//  2. ZRANGEBYSCORE 取 score <= now 的到期任务
//  3. 从协程池提交 Executor 协程执行每个任务
//  4. 完成后更新锁 TTL（延长过期时间，标记分片已处理）
type Trigger struct {
	queue    *redis.RedisQueue
	pool     *pool.GoWorkerPool
	executor *Executor
	cfg      *config.SchedulerConfig
}

// NewTrigger 创建触发器实例
func NewTrigger(
	queue *redis.RedisQueue,
	pool *pool.GoWorkerPool,
	executor *Executor,
	cfg *config.SchedulerConfig,
) *Trigger {
	return &Trigger{
		queue:    queue,
		pool:     pool,
		executor: executor,
		cfg:      cfg,
	}
}

// Run 在指定时间片内轮询指定桶的 Redis ZSet，取出到期任务并提交 Executor 执行
// timeRange: 时间范围标识（YYYY-MM-DD-HH:mm）
// bucket: 桶号
// 此方法由 Scheduler 通过协程池调用
func (t *Trigger) Run(ctx context.Context, timeRange string, bucket int) {
	start := time.Now()
	logger.Debug("Trigger 开始处理",
		zap.String("time_range", timeRange),
		zap.Int("bucket", bucket),
	)

	// 计算时间片结束时间（当前分钟结束）
	// 在时间片内持续轮询，直到时间片过期或无更多到期任务
	timeSliceEnd := calculateTimeSliceEnd(timeRange)

	// 每次批量取出的任务数量
	batchSize := int64(100)

	for {
		// 检查是否已超过时间片
		if time.Now().After(timeSliceEnd) {
			logger.Debug("Trigger 时间片结束",
				zap.String("time_range", timeRange),
				zap.Int("bucket", bucket),
				zap.Duration("elapsed", time.Since(start)),
			)
			break
		}

		// 检查 context 是否已取消
		select {
		case <-ctx.Done():
			logger.Debug("Trigger 因 context 取消而停止")
			return
		default:
		}

		// 从 Redis ZSet 原子弹出到期任务
		triggers, err := t.queue.PopDueTasks(ctx, timeRange, bucket, batchSize)
		if err != nil {
			logger.Error("Trigger 弹出到期任务失败",
				zap.String("time_range", timeRange),
				zap.Int("bucket", bucket),
				zap.Error(err),
			)
			// 短暂等待后重试
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// 无到期任务，短暂等待后继续轮询
		if len(triggers) == 0 {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		logger.Debug("Trigger 取出到期任务",
			zap.String("time_range", timeRange),
			zap.Int("bucket", bucket),
			zap.Int("count", len(triggers)),
		)

		// 为每个到期任务提交 Executor 协程
		for _, trigger := range triggers {
			triggerCopy := trigger // 捕获循环变量
			err := t.pool.Submit(func() {
				t.executor.Execute(ctx, triggerCopy)
			})
			if err != nil {
				logger.Error("Trigger 提交 Executor 到协程池失败",
					zap.Int64("timer_id", triggerCopy.TimerID),
					zap.Error(err),
				)
			}
		}

		// 续期锁 TTL，标记此分片正在处理中
		lockExpiration := time.Duration(t.cfg.ScanInterval*2) * time.Second
		if err := t.queue.ExtendSchedulerLock(ctx, timeRange, bucket, lockExpiration); err != nil {
			logger.Warn("Trigger 续期锁失败",
				zap.String("time_range", timeRange),
				zap.Int("bucket", bucket),
				zap.Error(err),
			)
		}
	}
}

// calculateTimeSliceEnd 计算时间片结束时间
// 时间片按分钟划分，结束时间是当前分钟的 59 秒
func calculateTimeSliceEnd(timeRange string) (t time.Time) {
	parsed, err := time.Parse("2006-01-02-15:04", timeRange)
	if err != nil {
		// 解析失败时返回当前时间 + 1 分钟
		return time.Now().Add(time.Minute)
	}
	// 时间片结束 = 当前分钟的 59 秒
	return parsed.Add(time.Minute).Add(-time.Second)
}
