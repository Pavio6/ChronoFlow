package service

import (
	"context"
	"encoding/json"
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

// TimerService 定时器业务逻辑层
// 封装定时器定义的 CRUD 操作和状态管理
type TimerService struct {
	defRepo    repository.TimerDefinitionRepository
	recRepo    repository.TimerRecordRepository
	parser     *cron.CronParser
	queue      *redis.RedisQueue
	schedCfg   *config.SchedulerConfig
}

// NewTimerService 创建定时器服务实例
func NewTimerService(
	defRepo repository.TimerDefinitionRepository,
	recRepo repository.TimerRecordRepository,
	parser *cron.CronParser,
	queue *redis.RedisQueue,
	schedCfg *config.SchedulerConfig,
) *TimerService {
	return &TimerService{
		defRepo:  defRepo,
		recRepo:  recRepo,
		parser:   parser,
		queue:    queue,
		schedCfg: schedCfg,
	}
}

// Create 创建定时器定义
// 验证 Cron 表达式合法性后创建，默认状态为 INACTIVE
func (s *TimerService) Create(req *model.CreateTimerDefinitionRequest) (*model.TimerDefinition, error) {
	// 验证 Cron 表达式
	if err := s.parser.ValidateCronExpr(req.CronExpr); err != nil {
		return nil, fmt.Errorf("无效的 Cron 表达式: %w", err)
	}

	// 序列化 callback_headers
	headersJSON := "{}"
	if req.CallbackHeaders != nil {
		b, err := json.Marshal(req.CallbackHeaders)
		if err != nil {
			return nil, fmt.Errorf("序列化回调头失败: %w", err)
		}
		headersJSON = string(b)
	}

	// 创建定时器定义
	def := &model.TimerDefinition{
		App:             req.App,
		Name:            req.Name,
		CronExpr:        req.CronExpr,
		CallbackURL:     req.CallbackURL,
		CallbackMethod:  req.CallbackMethod,
		CallbackBody:    req.CallbackBody,
		CallbackHeaders: headersJSON,
		Status:          model.TimerStatusInactive,
		Timeout:         req.Timeout,
	}

	if err := s.defRepo.Create(def); err != nil {
		return nil, fmt.Errorf("创建定时器失败: %w", err)
	}

	return def, nil
}

// GetByID 根据 ID 获取定时器定义
func (s *TimerService) GetByID(id int64) (*model.TimerDefinition, error) {
	def, err := s.defRepo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("查询定时器失败: %w", err)
	}
	if def == nil {
		return nil, fmt.Errorf("定时器不存在，id=%d", id)
	}
	return def, nil
}

// Update 更新定时器定义
// 使用指针字段实现部分更新
func (s *TimerService) Update(id int64, req *model.UpdateTimerDefinitionRequest) (*model.TimerDefinition, error) {
	// 查询现有定义
	def, err := s.defRepo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("查询定时器失败: %w", err)
	}
	if def == nil {
		return nil, fmt.Errorf("定时器不存在，id=%d", id)
	}

	// 部分更新
	if req.Name != nil {
		def.Name = *req.Name
	}
	if req.CronExpr != nil {
		// 验证新的 Cron 表达式
		if err := s.parser.ValidateCronExpr(*req.CronExpr); err != nil {
			return nil, fmt.Errorf("无效的 Cron 表达式: %w", err)
		}
		def.CronExpr = *req.CronExpr
	}
	if req.CallbackURL != nil {
		def.CallbackURL = *req.CallbackURL
	}
	if req.CallbackMethod != nil {
		def.CallbackMethod = *req.CallbackMethod
	}
	if req.CallbackBody != nil {
		def.CallbackBody = *req.CallbackBody
	}
	if req.CallbackHeaders != nil {
		b, err := json.Marshal(*req.CallbackHeaders)
		if err != nil {
			return nil, fmt.Errorf("序列化回调头失败: %w", err)
		}
		def.CallbackHeaders = string(b)
	}
	if req.Timeout != nil {
		def.Timeout = *req.Timeout
	}

	if err := s.defRepo.Update(def); err != nil {
		return nil, fmt.Errorf("更新定时器失败: %w", err)
	}

	return def, nil
}

// Delete 逻辑删除定时器定义
func (s *TimerService) Delete(id int64) error {
	if err := s.defRepo.Delete(id); err != nil {
		return fmt.Errorf("删除定时器失败: %w", err)
	}
	return nil
}

// List 分页查询定时器定义列表
func (s *TimerService) List(req *model.TimerDefinitionListRequest) (*model.TimerDefinitionListResponse, error) {
	// 设置默认分页参数
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 10
	}

	items, total, err := s.defRepo.List(req)
	if err != nil {
		return nil, fmt.Errorf("查询定时器列表失败: %w", err)
	}

	return &model.TimerDefinitionListResponse{
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
		Items:    items,
	}, nil
}

// Activate 激活定时器
// 状态转换：INACTIVE -> ACTIVE
// 激活时立即同步未来 step1_duration*2 时间窗口内的任务到 Redis
func (s *TimerService) Activate(id int64) error {
	def, err := s.defRepo.GetByID(id)
	if err != nil {
		return fmt.Errorf("查询定时器失败: %w", err)
	}
	if def == nil {
		return fmt.Errorf("定时器不存在，id=%d", id)
	}

	// 验证状态转换合法性
	if err := model.ValidateTransition(def.Status, model.TimerStatusActive); err != nil {
		return fmt.Errorf("无法激活定时器: %w", err)
	}

	// 计算时间窗口：now ~ now + migrate_step_minutes*2（取整到小时）
	now := time.Now()
	endTime := now.Add(time.Duration(s.schedCfg.MigrateStepMinutes*2) * time.Minute)
	endTime = time.Date(endTime.Year(), endTime.Month(), endTime.Day(), endTime.Hour(), 0, 0, 0, time.Local)

	// 计算时间窗口内的所有触发时间点
	triggerTimes, err := s.parser.NextTriggerTimesBefore(def.CronExpr, now, endTime)
	if err != nil {
		return fmt.Errorf("解析 Cron 表达式失败: %w", err)
	}

	// 按时间步统计任务数量，用于动态分桶
	taskCountByTimeRange := make(map[string]int)
	for _, triggerTime := range triggerTimes {
		timeRange := triggerTime.Format("2006-01-02-15:04")
		taskCountByTimeRange[timeRange]++
	}

	// 计算每个时间步的动态分桶数并存储到 Redis
	bucketNumByTimeRange := make(map[string]int)
	lockExpiration := time.Duration(s.schedCfg.MigrateStepMinutes*2) * time.Minute
	for timeRange, count := range taskCountByTimeRange {
		bucketNum := calculateBucketNum(count, s.schedCfg.BucketNum)
		bucketNumByTimeRange[timeRange] = bucketNum
		if err := s.queue.SetBucketNum(context.Background(), timeRange, bucketNum, lockExpiration); err != nil {
			logger.Error("激活时存储分桶映射失败",
				zap.String("time_range", timeRange),
				zap.Int("bucket_num", bucketNum),
				zap.Error(err),
			)
		}
	}

	// 按分桶收集待推送的触发信息
	// key 格式: {time_range}:{bucket}
	bucketTriggers := make(map[string][]*redis.TaskTrigger)
	for _, triggerTime := range triggerTimes {
		timeRange := triggerTime.Format("2006-01-02-15:04")
		bucketNum := bucketNumByTimeRange[timeRange]

		// 幂等性检查：避免重复创建记录
		exists, err := s.recRepo.ExistsByTimerIDAndTriggerTime(def.ID, triggerTime)
		if err != nil {
			logger.Error("激活时幂等检查失败",
				zap.Int64("timer_id", def.ID),
				zap.Time("trigger_time", triggerTime),
				zap.Error(err),
			)
			continue
		}
		if exists {
			continue
		}

		// 创建执行记录
		record := &model.TimerRecord{
			TimerID:       def.ID,
			TriggerTime:   triggerTime,
			Status:        model.RecordStatusPending,
			RequestURL:    def.CallbackURL,
			RequestMethod: def.CallbackMethod,
			RequestBody:   def.CallbackBody,
		}
		if err := s.recRepo.Create(record); err != nil {
			logger.Error("激活时创建执行记录失败",
				zap.Int64("timer_id", def.ID),
				zap.Time("trigger_time", triggerTime),
				zap.Error(err),
			)
			continue
		}

		// 计算分桶号（使用动态分桶）
		bucket := int(def.ID) % bucketNum
		key := fmt.Sprintf("%s:%d", timeRange, bucket)
		bucketTriggers[key] = append(bucketTriggers[key], &redis.TaskTrigger{
			TimerID:     def.ID,
			TriggerTime: triggerTime,
		})
	}

	// 批量推送到 Redis
	for key, triggers := range bucketTriggers {
		var timeRange string
		var bucket int
		if _, err := fmt.Sscanf(key, "%s:%d", &timeRange, &bucket); err != nil {
			logger.Error("激活时解析队列 key 失败",
				zap.String("key", key),
				zap.Error(err),
			)
			continue
		}
		if err := s.queue.BatchPushTasks(context.Background(), timeRange, bucket, triggers); err != nil {
			logger.Error("激活时批量推送任务到 Redis 失败",
				zap.String("key", key),
				zap.Int("count", len(triggers)),
				zap.Error(err),
			)
			return fmt.Errorf("同步任务到 Redis 失败: %w", err)
		}
	}

	// 更新状态为 ACTIVE
	return s.defRepo.UpdateStatus(id, model.TimerStatusActive)
}

// Deactivate 停用定时器
// 状态转换：ACTIVE -> INACTIVE
func (s *TimerService) Deactivate(id int64) error {
	def, err := s.defRepo.GetByID(id)
	if err != nil {
		return fmt.Errorf("查询定时器失败: %w", err)
	}
	if def == nil {
		return fmt.Errorf("定时器不存在，id=%d", id)
	}

	// 验证状态转换合法性
	if err := model.ValidateTransition(def.Status, model.TimerStatusInactive); err != nil {
		return fmt.Errorf("无法停用定时器: %w", err)
	}

	return s.defRepo.UpdateStatus(id, model.TimerStatusInactive)
}

// GetRecords 获取指定定时器的执行记录
func (s *TimerService) GetRecords(timerID int64, limit int) ([]*model.TimerRecord, error) {
	if limit <= 0 {
		limit = 20
	}

	records, err := s.recRepo.GetByTimerID(timerID, limit)
	if err != nil {
		return nil, fmt.Errorf("查询执行记录失败: %w", err)
	}

	return records, nil
}

// ListRecords 分页查询执行记录列表
func (s *TimerService) ListRecords(req *model.RecordListRequest) (*model.RecordListResponse, error) {
	// 设置默认分页参数
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 10
	}

	items, total, err := s.recRepo.List(req)
	if err != nil {
		return nil, fmt.Errorf("查询执行记录列表失败: %w", err)
	}

	return &model.RecordListResponse{
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
		Items:    items,
	}, nil
}

// calculateBucketNum 根据任务数量计算分桶数
// 规则：bucket_num = min(max(task_count / 100, 1), max_bucket)
func calculateBucketNum(taskCount int, maxBucket int) int {
	// 每 100 个任务一个桶，最少 1 个桶
	bucketNum := taskCount / 100
	if bucketNum < 1 {
		bucketNum = 1
	}
	// 不超过最大桶数
	if bucketNum > maxBucket {
		bucketNum = maxBucket
	}
	return bucketNum
}
