package service

import (
	"context"
	"fmt"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/pkg/pool"
	"github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/pkg/logger"
	"go.uber.org/zap"
)

// Scheduler 调度器（对应 xTimer 的 Scheduler 模块）
// 每隔 scan_interval（默认 1 秒）轮询一次，为每个二维分片抢分布式锁
// 抢到锁后从协程池提交 Trigger 协程处理该分片
// 核心流程：
//  1. 计算当前分钟级时间范围 time_range
//  2. 遍历桶号 0 ~ bucketNum-1
//  3. 对每个桶尝试 SETNX 抢锁，TTL < 时间片时长
//  4. 抢到锁 → 从协程池提交 Trigger 协程
type Scheduler struct {
	queue   *redis.RedisQueue
	pool    *pool.GoWorkerPool
	trigger *Trigger
	cfg     *config.SchedulerConfig
	quit    chan struct{}
}

// NewScheduler 创建调度器实例
func NewScheduler(
	queue *redis.RedisQueue,
	pool *pool.GoWorkerPool,
	trigger *Trigger,
	cfg *config.SchedulerConfig,
) *Scheduler {
	return &Scheduler{
		queue:   queue,
		pool:    pool,
		trigger: trigger,
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
// 计算当前时间范围，遍历每个桶，抢锁成功后提交 Trigger
func (s *Scheduler) schedule(ctx context.Context) {
	// 计算当前分钟级时间范围
	now := time.Now()
	timeRange := formatTimeRange(now)

	// 锁的过期时间设为 scan_interval 的 2 倍，防止锁提前过期
	lockExpiration := time.Duration(s.cfg.ScanInterval*2) * time.Second

	// 遍历每个桶
	for bucket := 0; bucket < s.cfg.BucketNum; bucket++ {
		// 尝试获取分布式锁
		acquired, err := s.queue.AcquireSchedulerLock(ctx, timeRange, bucket, lockExpiration)
		if err != nil {
			logger.Error("Scheduler 获取锁失败",
				zap.String("time_range", timeRange),
				zap.Int("bucket", bucket),
				zap.Error(err),
			)
			continue
		}

		// 未获取到锁，跳过此桶（其他实例正在处理）
		if !acquired {
			continue
		}

		// 获取到锁，从协程池提交 Trigger 协程
		bucketCopy := bucket // 捕获循环变量
		err = s.pool.Submit(func() {
			s.trigger.Run(ctx, timeRange, bucketCopy)
		})
		if err != nil {
			logger.Error("Scheduler 提交 Trigger 到协程池失败",
				zap.String("time_range", timeRange),
				zap.Int("bucket", bucketCopy),
				zap.Error(err),
			)
			// 提交失败时释放锁
			if releaseErr := s.queue.ReleaseSchedulerLock(ctx, timeRange, bucketCopy); releaseErr != nil {
				logger.Error("Scheduler 释放锁失败",
					zap.String("time_range", timeRange),
					zap.Int("bucket", bucketCopy),
					zap.Error(releaseErr),
				)
			}
		}
	}
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
