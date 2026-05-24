package service

import (
	"context"
	"fmt"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/pkg/pool"
	"github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/pkg/logger"
	"go.uber.org/zap"
)

// Trigger 触发器
// 被 Scheduler 启动后，在时间片内持续轮询 Redis ZSet
// 读取到期任务后从协程池提交 Executor 协程执行
// 核心流程：
//  1. 在时间片内持续轮询 Redis ZSet {time_range}:{bucket}
//  2. 每轮循环续期锁 TTL（参考 xTimer successExpireSeconds），防止空闲时锁过期
//  3. ZRANGEBYSCORE 获取 score <= now 的到期任务（只读不删）
//  4. 若 Redis 无结果，回退查询 MySQL（冷启动兜底）
//  5. 从协程池提交 Executor 协程执行每个任务
// 防重复机制：任务保留在 ZSet 中，依赖分布式锁保证同一分片同一时刻只有一个节点处理
// DB 回退：冷启动时 Migrator 尚未预创建 Redis 数据，Trigger 从 MySQL 读取 PENDING 记录兜底
type Trigger struct {
	queue    *redis.RedisQueue
	pool     *pool.GoWorkerPool
	executor *Executor
	recRepo  repository.TimerRecordRepository
	cfg      *config.SchedulerConfig
}

// NewTrigger 创建触发器实例
func NewTrigger(
	queue *redis.RedisQueue,
	pool *pool.GoWorkerPool,
	executor *Executor,
	recRepo repository.TimerRecordRepository,
	cfg *config.SchedulerConfig,
) *Trigger {
	return &Trigger{
		queue:    queue,
		pool:     pool,
		executor: executor,
		recRepo:  recRepo,
		cfg:      cfg,
	}
}

// Run 在指定时间片内轮询指定桶的 Redis ZSet，取出到期任务并提交 Executor 执行
// timeRange: 时间范围标识（YYYY-MM-DD-HH:mm）
// bucket: 桶号
// bucketNum: 总桶数（用于 DB 回退时按 timer_id % bucketNum 过滤）
// 此方法由 Scheduler 通过协程池调用
func (t *Trigger) Run(ctx context.Context, timeRange string, bucket int, bucketNum int) {
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

	// 锁续期时间，参考 xTimer successExpireSeconds
	// 每轮循环续期一次，防止空闲时锁提前过期导致并发重复执行
	successExpiration := time.Duration(t.cfg.SuccessExpiration) * time.Second

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

		// 每轮循环续期锁 TTL，防止空闲时锁过期
		if err := t.queue.ExtendSchedulerLock(ctx, timeRange, bucket, successExpiration); err != nil {
			logger.Warn("Trigger 续期锁失败",
				zap.String("time_range", timeRange),
				zap.Int("bucket", bucket),
				zap.Error(err),
			)
		}

		// 从 Redis ZSet 获取到期任务（只读不删，依赖分布式锁防重）
		triggers, err := t.queue.GetDueTasks(ctx, timeRange, bucket, batchSize)
		if err != nil {
			logger.Error("Trigger 获取到期任务失败",
				zap.String("time_range", timeRange),
				zap.Int("bucket", bucket),
				zap.Error(err),
			)
			// 短暂等待后重试
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Redis 无结果时，回退查询 MySQL（冷启动兜底）
		if len(triggers) == 0 {
			triggers, err = t.getDueTasksFromDB(ctx, timeRange, bucket, bucketNum)
			if err != nil {
				logger.Error("Trigger DB 回退查询失败",
					zap.String("time_range", timeRange),
					zap.Int("bucket", bucket),
					zap.Error(err),
				)
			}
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

// getDueTasksFromDB DB 回退：当 Redis 缓存 miss 时，从 MySQL 查询 PENDING 记录
// 过滤条件：status = PENDING, trigger_time 在当前分钟范围内, timer_id % bucketNum == bucket
func (t *Trigger) getDueTasksFromDB(ctx context.Context, timeRange string, bucket int, bucketNum int) ([]*redis.TaskTrigger, error) {
	// 解析时间范围
	start, err := time.Parse("2006-01-02-15:04", timeRange)
	if err != nil {
		return nil, fmt.Errorf("解析时间范围失败: %w", err)
	}
	end := start.Add(time.Minute)

	// 查询 PENDING 记录
	records, err := t.recRepo.GetPendingByTimeRange(start, end)
	if err != nil {
		return nil, err
	}

	// 按 bucket 过滤并转换为 TaskTrigger
	var triggers []*redis.TaskTrigger
	for _, record := range records {
		if int(record.TimerID)%bucketNum != bucket {
			continue
		}
		triggers = append(triggers, &redis.TaskTrigger{
			TimerID:     record.TimerID,
			TriggerTime: record.TriggerTime,
		})
	}

	if len(triggers) > 0 {
		logger.Debug("Trigger DB 回退命中",
			zap.String("time_range", timeRange),
			zap.Int("bucket", bucket),
			zap.Int("count", len(triggers)),
		)
	}

	return triggers, nil
}
