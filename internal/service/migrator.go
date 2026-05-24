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

// Migrator 数据迁移器
// 负责将 MySQL 中的定时器定义批量预创建为定时任务记录，并推送到 Redis ZSet 队列
// 执行时机：每隔 migrate_step_minutes 触发一次（启动时不立即执行，等第一次 ticker）
// 核心流程：
//  1. 全量扫描 MySQL 中 ACTIVE 状态的定时器定义
//  2. 以小时级精度计算下一个 step1 时间范围内的所有触发时间点
//  3. 批量插入 timer_records 到 MySQL
//  4. 按 {time_range}:{bucket} 分组批量 ZAdd 到 Redis
//
// 冷启动覆盖：启动后第一个 step1 时间窗口内的任务由 Trigger 的 DB 回退机制处理
type Migrator struct {
	defRepo repository.TimerDefinitionRepository
	recRepo repository.TimerRecordRepository
	queue   *redis.RedisQueue
	parser  *cron.CronParser
	cfg     *config.SchedulerConfig
	quit    chan struct{}
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
// 等待第一次 ticker 触发后执行，然后每隔 migrate_step_minutes 定时执行
// 冷启动期间的任务由 Trigger 的 DB 回退机制兜底
func (m *Migrator) Start(ctx context.Context) {
	logger.Info("Migrator 启动",
		zap.Int("migrate_step_minutes", m.cfg.MigrateStepMinutes),
		zap.Int("bucket_num", m.cfg.BucketNum),
	)

	// 定时执行迁移（等待第一次 ticker 触发）
	ticker := time.NewTicker(time.Duration(m.cfg.MigrateStepMinutes) * time.Minute)
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

	// 计算迁移的时间范围：小时级取整
	// start = GetStartHour(now + step), end = GetStartHour(now + 2*step)
	// 例如 10:00 执行，step=60min → start=11:00, end=12:00 → 窗口 [11:00, 12:00)
	// 例如 10:30 执行，step=60min → start=11:00, end=12:00 → 窗口 [11:00, 12:00)
	now := time.Now()
	startTime := getStartHour(now.Add(time.Duration(m.cfg.MigrateStepMinutes) * time.Minute))
	endTime := getStartHour(now.Add(time.Duration(m.cfg.MigrateStepMinutes*2) * time.Minute))

	// 按时间步统计任务数量，用于动态分桶
	// key: timeRange, value: 任务数量
	taskCountByTimeRange := make(map[string]int)

	// 按分桶收集待推送的触发信息
	// key 格式: {time_range}:{bucket}
	bucketTriggers := make(map[string][]*redis.TaskTrigger)
	totalRecords := 0

	for _, def := range definitions {
		// 解析定时器在时间范围内的所有触发时间点
		triggerTimes, err := m.parser.NextTriggerTimesBefore(def.CronExpr, startTime, endTime)
		if err != nil {
			logger.Error("Migrator 解析 Cron 表达式失败",
				zap.Int64("timer_id", def.ID),
				zap.String("cron_expr", def.CronExpr),
				zap.Error(err),
			)
			continue
		}

		for _, triggerTime := range triggerTimes {

			// 幂等性检查：避免重复创建记录
			exists, err := m.recRepo.ExistsByTimerIDAndTriggerTime(def.ID, triggerTime)
			if err != nil {
				logger.Error("Migrator 幂等性检查失败",
					zap.Int64("timer_id", def.ID),
					zap.Time("trigger_time", triggerTime),
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
					zap.Time("trigger_time", triggerTime),
					zap.Error(err),
				)
				continue
			}

			// 计算时间范围标识（格式：YYYY-MM-DD-HH:mm）
			timeRange := formatTimeRange(triggerTime)

			// 统计每个时间步的任务数量
			taskCountByTimeRange[timeRange]++

			// 收集触发信息（分桶号稍后根据动态分桶计算）
			bucketTriggers[timeRange] = append(bucketTriggers[timeRange], &redis.TaskTrigger{
				TimerID:     def.ID,
				TriggerTime: triggerTime,
			})

			totalRecords++
		}
	}

	// 计算每个时间步的动态分桶数并存储到 Redis
	// 分桶规则：bucket_num = min(max(task_count / 100, 1), max_bucket)
	bucketNumByTimeRange := make(map[string]int)
	lockExpiration := time.Duration(m.cfg.MigrateStepMinutes*2) * time.Minute
	for timeRange, count := range taskCountByTimeRange {
		bucketNum := m.calculateBucketNum(count)
		bucketNumByTimeRange[timeRange] = bucketNum

		// 存储分桶映射到 Redis
		if err := m.queue.SetBucketNum(ctx, timeRange, bucketNum, lockExpiration); err != nil {
			logger.Error("Migrator 存储分桶映射失败",
				zap.String("time_range", timeRange),
				zap.Int("bucket_num", bucketNum),
				zap.Error(err),
			)
		}
	}

	// 按动态分桶号重新分组
	// key 格式: {time_range}:{bucket}
	dynamicBucketTriggers := make(map[string][]*redis.TaskTrigger)
	for timeRange, triggers := range bucketTriggers {
		bucketNum := bucketNumByTimeRange[timeRange]
		for _, trigger := range triggers {
			bucket := int(trigger.TimerID) % bucketNum
			key := fmt.Sprintf("%s:%d", timeRange, bucket)
			dynamicBucketTriggers[key] = append(dynamicBucketTriggers[key], trigger)
		}
	}

	// 批量推送到 Redis ZSet
	for key, triggers := range dynamicBucketTriggers {
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

// calculateBucketNum 根据任务数量计算分桶数
// 规则：bucket_num = min(max(task_count / 100, 1), max_bucket)
func (m *Migrator) calculateBucketNum(taskCount int) int {
	// 每 100 个任务一个桶，最少 1 个桶
	bucketNum := taskCount / 100
	if bucketNum < 1 {
		bucketNum = 1
	}
	// 不超过最大桶数
	if bucketNum > m.cfg.BucketNum {
		bucketNum = m.cfg.BucketNum
	}
	return bucketNum
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

// getStartHour 将时间取整到小时
// 例如 10:35:22 → 10:00:00
func getStartHour(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
}
