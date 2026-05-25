package service

import (
	"context"
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

	// 只收集本轮真正创建成功的记录；Redis 会原子累计分钟级投递规模并返回最终桶数。
	triggersByMinute := make(map[string][]*redis.TaskTrigger)
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

			triggersByMinute[timeRange] = append(triggersByMinute[timeRange], &redis.TaskTrigger{
				TimerID:     def.ID,
				TriggerTime: triggerTime,
			})

			totalRecords++
		}
	}

	if err := pushMinuteTriggers(ctx, m.queue, m.cfg, triggersByMinute); err != nil {
		// MySQL 中的 PENDING 记录保留为事实来源，Trigger 会在执行窗口合并查询恢复。
		logger.Error("Migrator 推送任务到 Redis 失败", zap.Error(err))
	}

	logger.Info("Migrator 迁移完成",
		zap.Int("definitions", len(definitions)),
		zap.Int("records", totalRecords),
		zap.Duration("elapsed", time.Since(start)),
	)
}

// formatTimeRange 格式化时间范围标识
// 格式：YYYY-MM-DD-HH:mm（分钟级精度）
func formatTimeRange(t time.Time) string {
	return t.Format("2006-01-02-15:04")
}

// getStartHour 将时间取整到小时
// 例如 10:35:22 → 10:00:00
func getStartHour(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
}
