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

// Scheduler 调度器
// 每隔 scan_interval（默认 1 秒）轮询一次，为每个二维分片抢分布式锁
// 抢到锁后从协程池提交 Trigger 协程处理该分片
// 核心流程：
//  1. 计算当前分钟级时间范围 time_range
//  2. 分别获取当前分钟和上一分钟的动态分桶数
//  3. 对每个桶尝试 SETNX 抢锁，TTL 由 lock_expiration 配置（默认 70 秒，大于时间片时长）
//  4. 抢到锁 → 从协程池提交 Trigger 协程
type Scheduler struct {
	queue   *redis.RedisQueue
	pool    *pool.GoWorkerPool
	trigger *Trigger
	recRepo repository.TimerRecordRepository
	cfg     *config.SchedulerConfig
	quit    chan struct{}
}

// NewScheduler 创建调度器实例
func NewScheduler(
	queue *redis.RedisQueue,
	pool *pool.GoWorkerPool,
	trigger *Trigger,
	recRepo repository.TimerRecordRepository,
	cfg *config.SchedulerConfig,
) *Scheduler {
	return &Scheduler{
		queue:   queue,
		pool:    pool,
		trigger: trigger,
		recRepo: recRepo,
		cfg:     cfg,
		quit:    make(chan struct{}),
	}
}

// Start 启动调度器
// 每隔 scan_interval 秒轮询一次，为每个分片抢锁并提交 Trigger
func (s *Scheduler) Start(ctx context.Context) {
	logger.Info("Scheduler 启动",
		zap.Int("scan_interval", s.cfg.ScanInterval),
		zap.Int("bucket_num", s.cfg.BucketNum),
	)

	ticker := time.NewTicker(time.Duration(s.cfg.ScanInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.schedule(ctx)
		case <-s.quit:
			logger.Info("Scheduler 停止")
			return
		case <-ctx.Done():
			logger.Info("Scheduler 因 context 取消而停止")
			return
		}
	}
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	close(s.quit)
}

// schedule 执行一轮调度
// 同时处理当前分钟和上一分钟，避免边界任务遗漏
func (s *Scheduler) schedule(ctx context.Context) {
	now := time.Now()

	// 计算当前分钟和上一分钟的时间范围
	currentTimeRange := formatTimeRange(now)
	prevTimeRange := formatTimeRange(now.Add(-time.Minute))

	// 从 Redis 获取当前分钟的动态分桶数
	currentBucketNum, err := s.queue.GetBucketNum(ctx, currentTimeRange, s.cfg.BucketNum)
	if err != nil {
		logger.Error("Scheduler 获取当前分钟动态分桶数失败，使用默认值",
			zap.String("time_range", currentTimeRange),
			zap.Error(err),
		)
		currentBucketNum = s.cfg.BucketNum
	}

	// 从 Redis 获取上一分钟的动态分桶数（可能与当前分钟不同）
	prevBucketNum, err := s.queue.GetBucketNum(ctx, prevTimeRange, s.cfg.BucketNum)
	if err != nil {
		logger.Error("Scheduler 获取上一分钟动态分桶数失败，使用默认值",
			zap.String("time_range", prevTimeRange),
			zap.Error(err),
		)
		prevBucketNum = s.cfg.BucketNum
	}

	// 锁的初始 TTL，参考 xTimer tryLockSeconds（必须大于时间片时长 60 秒）
	lockExpiration := time.Duration(s.cfg.LockExpiration) * time.Second

	// 处理当前分钟
	for bucket := 0; bucket < currentBucketNum; bucket++ {
		s.handleSlice(ctx, currentTimeRange, bucket, currentBucketNum, lockExpiration)
	}
	// 处理上一分钟，避免边界任务遗漏
	for bucket := 0; bucket < prevBucketNum; bucket++ {
		s.handleSlice(ctx, prevTimeRange, bucket, prevBucketNum, lockExpiration)
	}
}

// handleSlice 处理单个时间片的单个桶：抢锁 → 提交 Trigger
func (s *Scheduler) handleSlice(ctx context.Context, timeRange string, bucket int, bucketNum int, lockExpiration time.Duration) {
	hasWork, err := s.hasSliceWork(ctx, timeRange, bucket, bucketNum)
	if err != nil {
		logger.Error("Scheduler 检查分片任务失败",
			zap.String("time_range", timeRange),
			zap.Int("bucket", bucket),
			zap.Error(err),
		)
		return
	}
	if !hasWork {
		return
	}

	// 尝试获取分布式锁
	acquired, err := s.queue.AcquireSchedulerLock(ctx, timeRange, bucket, lockExpiration)
	if err != nil {
		logger.Error("Scheduler 获取锁失败",
			zap.String("time_range", timeRange),
			zap.Int("bucket", bucket),
			zap.Error(err),
		)
		return
	}

	// 未获取到锁，跳过（其他实例正在处理）
	if !acquired {
		return
	}

	// 获取到锁，从协程池提交 Trigger 协程
	bucketCopy := bucket
	timeRangeCopy := timeRange
	err = s.pool.Submit(func() {
		s.trigger.Run(ctx, timeRangeCopy, bucketCopy, bucketNum)
	})
	if err != nil {
		logger.Error("Scheduler 提交 Trigger 到协程池失败",
			zap.String("time_range", timeRangeCopy),
			zap.Int("bucket", bucketCopy),
			zap.Error(err),
		)
		// 提交失败时释放锁
		if releaseErr := s.queue.ReleaseSchedulerLock(ctx, timeRangeCopy, bucketCopy); releaseErr != nil {
			logger.Error("Scheduler 释放锁失败",
				zap.String("time_range", timeRangeCopy),
				zap.Int("bucket", bucketCopy),
				zap.Error(releaseErr),
			)
		}
	}
}

// hasSliceWork 判断分片是否有实际任务，避免空分片也创建 scheduler_lock
func (s *Scheduler) hasSliceWork(ctx context.Context, timeRange string, bucket int, bucketNum int) (bool, error) {
	exists, err := s.queue.QueueExists(ctx, timeRange, bucket)
	if err != nil {
		return false, err
	}
	if exists {
		return true, nil
	}

	start, err := time.Parse("2006-01-02-15:04", timeRange)
	if err != nil {
		return false, fmt.Errorf("解析时间范围失败: %w", err)
	}
	return s.recRepo.HasPendingByTimeRangeAndBucket(start, start.Add(time.Minute), bucket, bucketNum)
}

// formatTimeRange 格式化时间范围标识（分钟级精度）
// 格式：YYYY-MM-DD-HH:mm
func formatTimeRangeForScheduler(t time.Time) string {
	return t.Format("2006-01-02-15:04")
}

// buildQueueKeyDebug 构建队列 key 的调试信息
func buildQueueKeyDebug(timeRange string, bucket int) string {
	return fmt.Sprintf("%s%s:%d", redis.TaskQueuePrefix, timeRange, bucket)
}
