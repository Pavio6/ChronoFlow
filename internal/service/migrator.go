package service

import (
	"context"
	"fmt"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/cron"
	"github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/pkg/logger"
	"go.uber.org/zap"
)

// Migrator 数据迁移器（对应 xTimer 的一级迁移模块）
// 负责将 MySQL 中的定时器定义批量预创建为定时任务记录，并推送到 Redis ZSet 队列
// 执行时机：每隔 step1_duration 触发一次
// 核心流程：
//  1. 全量扫描 MySQL 中 ACTIVE 状态的定时器定义
//  2. 解析下一个 step1 时间范围内的所有触发时间点
//  3. 批量插入 timer_records 到 MySQL
//  4. 按 {time_range}:{bucket} 分组批量 ZAdd 到 Redis
type Migrator struct {
	defRepo  repository.TimerDefinitionRepository
	recRepo  repository.TimerRecordRepository
	queue    *redis.RedisQueue
	parser   *cron.CronParser
	cfg      *config.SchedulerConfig
	quit     chan struct{}
}

// NewMigrator 创建迁移器实例
func NewMigrator(
	defRepo repository.TimerDefinitionRepository,
	recRepo repository.TimerRecordRepository,
	queue *redis.RedisQueue,
	parser *cron.CronParser,
	cfg *config.SchedulerConfig,
) *Migrator {
	return &Migrator{
		defRepo: defRepo,
		recRepo: recRepo,
		queue:   queue,
		parser:  parser,
		cfg:     cfg,
		quit:    make(chan struct{}),
	}
}

// Start 启动迁移器
// 立即执行一次迁移，然后每隔 step1_duration 定时执行
func (m *Migrator) Start(ctx context.Context) {
	logger.Info("Migrator 启动",
		zap.Int("step1_duration", m.cfg.Step1Duration),
		zap.Int("bucket_num", m.cfg.BucketNum),
	)

	// 启动时立即执行一次迁移
	m.doMigrate(ctx)

	// 定时执行迁移
	ticker := time.NewTicker(time.Duration(m.cfg.Step1Duration) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.doMigrate(ctx)
		case <-m.quit:
			logger.Info("Migrator 停止")
			return
		case <-ctx.Done():
			logger.Info("Migrator 因 context 取消而停止")
			return
		}
	}
}

// Stop 停止迁移器
func (m *Migrator) Stop() {
	close(m.quit)
}

// doMigrate 执行一次完整的迁移流程
// 1. 查询所有 ACTIVE 状态的定时器定义
// 2. 对每个定时器，计算下一个 step1 时间范围内的所有触发时间点
// 3. 批量创建 timer_records 记录
// 4. 按分桶规则批量推送到 Redis ZSet
func (m *Migrator) doMigrate(ctx context.Context) {
	start := time.Now()
	logger.Info("Migrator 开始迁移")

	// 查询所有激活状态的定时器定义
	definitions, err := m.defRepo.GetActiveDefinitions()
	if err != nil {
		logger.Error("Migrator 查询激活定时器失败", zap.Error(err))
		return
	}

	if len(definitions) == 0 {
		logger.Info("Migrator 无激活定时器，跳过迁移")
		return
	}

	// 计算迁移的时间范围：当前时间到当前时间 + step1_duration
	now := time.Now()
	endTime := now.Add(time.Duration(m.cfg.Step1Duration) * time.Second)

	// 按分桶收集待推送的触发信息
	// key 格式: {time_range}:{bucket}
	bucketTriggers := make(map[string][]*redis.TaskTrigger)
	totalRecords := 0

	for _, def := range definitions {
		// 解析定时器在时间范围内的所有触发时间点
		triggerTimes, err := m.parser.NextNTriggerTimes(def.CronExpr, now, m.calculateMaxTriggers(now, endTime, def.CronExpr))
		if err != nil {
			logger.Error("Migrator 解析 Cron 表达式失败",
				zap.Int64("timer_id", def.ID),
				zap.String("cron_expr", def.CronExpr),
				zap.Error(err),
			)
			continue
		}

		// 过滤出在时间范围内的触发时间点
		for _, triggerTime := range triggerTimes {
			if triggerTime.After(endTime) {
				break
			}

			// 幂等性检查：避免重复创建记录
			exists, err := m.recRepo.ExistsByTimerIDAndTriggerTime(def.ID, triggerTime)
			if err != nil {
				logger.Error("Migrator 幂等性检查失败",
					zap.Int64("timer_id", def.ID),
					zap.Error(err),
				)
				continue
			}
			if exists {
				continue
			}

			// 创建定时器执行记录
			record := &model.TimerRecord{
				TimerID:       def.ID,
				TriggerTime:   triggerTime,
				Status:        model.RecordStatusPending,
				RequestURL:    def.CallbackURL,
				RequestMethod: def.CallbackMethod,
				RequestBody:   def.CallbackBody,
			}

			if err := m.recRepo.Create(record); err != nil {
				logger.Error("Migrator 创建执行记录失败",
					zap.Int64("timer_id", def.ID),
					zap.Error(err),
				)
				continue
			}

			// 计算分桶号：timer_id % bucket_num
			bucket := int(def.ID) % m.cfg.BucketNum

			// 计算时间范围标识（格式：YYYY-MM-DD-HH:mm）
			timeRange := formatTimeRange(triggerTime)

			// 构建 Redis 队列的 key
			key := fmt.Sprintf("%s:%d", timeRange, bucket)

			// 收集触发信息
			bucketTriggers[key] = append(bucketTriggers[key], &redis.TaskTrigger{
				TimerID:     def.ID,
				TriggerTime: triggerTime,
			})

			totalRecords++
		}
	}

	// 批量推送到 Redis ZSet
	for key, triggers := range bucketTriggers {
		// 解析 key 获取 timeRange 和 bucket
		timeRange, bucket, err := parseQueueKey(key)
		if err != nil {
			logger.Error("Migrator 解析队列 key 失败",
				zap.String("key", key),
				zap.Error(err),
			)
			continue
		}

		if err := m.queue.BatchPushTasks(ctx, timeRange, bucket, triggers); err != nil {
			logger.Error("Migrator 批量推送任务到 Redis 失败",
				zap.String("key", key),
				zap.Int("count", len(triggers)),
				zap.Error(err),
			)
			continue
		}
	}

	logger.Info("Migrator 迁移完成",
		zap.Int("definitions", len(definitions)),
		zap.Int("records", totalRecords),
		zap.Duration("elapsed", time.Since(start)),
	)
}

// calculateMaxTriggers 计算时间范围内最大可能的触发次数
// 用于限制 NextNTriggerTimes 的 n 参数，避免计算过多
func (m *Migrator) calculateMaxTriggers(from, to time.Time, cronExpr string) int {
	// 估算：时间范围 / 最小间隔（假设最小 1 秒一次）
	duration := to.Sub(from)
	maxTriggers := int(duration.Seconds()) + 1
	// 上限保护
	if maxTriggers > 10000 {
		maxTriggers = 10000
	}
	return maxTriggers
}

// formatTimeRange 格式化时间范围标识
// 格式：YYYY-MM-DD-HH:mm（分钟级精度）
func formatTimeRange(t time.Time) string {
	return t.Format("2006-01-02-15:04")
}

// parseQueueKey 解析队列 key，提取 timeRange 和 bucket
// key 格式: {time_range}:{bucket}
func parseQueueKey(key string) (string, int, error) {
	var timeRange string
	var bucket int
	// 从右向左找最后一个冒号
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == ':' {
			timeRange = key[:i]
			_, err := fmt.Sscanf(key[i+1:], "%d", &bucket)
			if err != nil {
				return "", 0, fmt.Errorf("解析 bucket 失败: %w", err)
			}
			return timeRange, bucket, nil
		}
	}
	return "", 0, fmt.Errorf("无效的队列 key 格式: %s", key)
}
