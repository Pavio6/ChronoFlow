package service

import (
	"context"
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
//  2. 以不重叠的 [cursor, dueEnd) 时间窗口读取到期任务
//  3. 分片扫描成功后将锁保留 success_expiration，抑制上一分钟回扫重入
//  4. 每个窗口合并 MySQL PENDING 记录，覆盖 Redis 部分投递失败
//  5. 从协程池提交 Executor 协程执行每个任务
//
// 防重复机制：窗口游标避免单次扫描重复派发，Executor 原子状态抢占负责最终防重
// DB 补偿：MySQL 是事实来源，Trigger 合并已有 PENDING 记录以覆盖 Redis 缓存不完整
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
// bucketNum: 总桶数（用于 DB 补偿时按 timer_id % bucketNum 过滤）
// lock: Scheduler worker 获取的、带所有者 token 的分片锁
// 此方法由 Scheduler 通过协程池调用
func (t *Trigger) Run(ctx context.Context, timeRange string, bucket int, bucketNum int, lock *redis.SchedulerLock) {
	start := time.Now()
	logger.Debug("Trigger 开始处理",
		zap.String("time_range", timeRange),
		zap.Int("bucket", bucket),
	)

	timeSliceStart, err := time.ParseInLocation("2006-01-02-15:04", timeRange, time.Local)
	if err != nil {
		logger.Error("Trigger 解析时间片失败", zap.String("time_range", timeRange), zap.Error(err))
		return
	}
	timeSliceEnd := timeSliceStart.Add(time.Minute)
	cursor := timeSliceStart

	// 与 xTimer 一致，完整分片成功扫描后才将锁延长为成功保留 TTL。
	successExpiration := time.Duration(t.cfg.SuccessExpiration) * time.Second
	completed := true

	for cursor.Before(timeSliceEnd) {
		// 检查 context 是否已取消
		select {
		case <-ctx.Done():
			logger.Debug("Trigger 因 context 取消而停止")
			return
		default:
		}

		dueEnd := time.Now().Truncate(time.Second).Add(time.Second)
		if dueEnd.After(timeSliceEnd) {
			dueEnd = timeSliceEnd
		}
		if !cursor.Before(dueEnd) {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// 只扫描尚未处理的半开区间，ZSet 中的数据无需为派发防重而删除。
		triggers, err := t.queue.GetTasksByTime(ctx, timeRange, bucket, cursor, dueEnd)
		if err != nil {
			completed = false
			logger.Error("Trigger 获取到期任务失败",
				zap.String("time_range", timeRange),
				zap.Int("bucket", bucket),
				zap.Error(err),
			)
			// 短暂等待后进入下一轮扫描
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// MySQL 是事实来源；即便 Redis 返回了部分任务，也必须合并未投递成功的 PENDING 记录。
		dbTriggers, err := t.getDueTasksFromDB(ctx, timeRange, bucket, bucketNum, cursor, dueEnd)
		if err != nil {
			completed = false
			logger.Error("Trigger DB 补偿查询失败",
				zap.String("time_range", timeRange),
				zap.Int("bucket", bucket),
				zap.Error(err),
			)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		triggers = mergeTaskTriggers(triggers, dbTriggers)

		if len(triggers) > 0 {
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
					completed = false
					logger.Error("Trigger 提交 Executor 到协程池失败",
						zap.Int64("timer_id", triggerCopy.TimerID),
						zap.Error(err),
					)
				}
			}
		}
		cursor = dueEnd
	}

	if completed {
		if err := lock.Extend(ctx, successExpiration); err != nil {
			logger.Warn("Trigger 完成后保留锁失败",
				zap.String("time_range", timeRange),
				zap.Int("bucket", bucket),
				zap.Error(err),
			)
		}
	}

	logger.Debug("Trigger 时间片扫描完成",
		zap.String("time_range", timeRange),
		zap.Int("bucket", bucket),
		zap.Bool("completed", completed),
		zap.Duration("elapsed", time.Since(start)),
	)
}

// getDueTasksFromDB DB 补偿：从 MySQL 查询扫描窗口内的 PENDING 记录
// 过滤条件：status = PENDING, trigger_time 在当前扫描窗口内, timer_id % bucketNum == bucket
func (t *Trigger) getDueTasksFromDB(ctx context.Context, timeRange string, bucket int, bucketNum int, start, end time.Time) ([]*redis.TaskTrigger, error) {
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
		logger.Debug("Trigger DB 补偿命中",
			zap.String("time_range", timeRange),
			zap.Int("bucket", bucket),
			zap.Int("count", len(triggers)),
		)
	}

	return triggers, nil
}

func mergeTaskTriggers(groups ...[]*redis.TaskTrigger) []*redis.TaskTrigger {
	type triggerKey struct {
		timerID     int64
		triggerTime int64
	}

	merged := make([]*redis.TaskTrigger, 0)
	seen := make(map[triggerKey]struct{})
	for _, group := range groups {
		for _, trigger := range group {
			key := triggerKey{timerID: trigger.TimerID, triggerTime: trigger.TriggerTime.UnixMilli()}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, trigger)
		}
	}
	return merged
}
