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
// 封装不可变定时器定义的创建、读取和状态管理
type TimerService struct {
	defRepo  repository.TimerDefinitionRepository
	recRepo  repository.TimerRecordRepository
	parser   *cron.CronParser
	queue    *redis.RedisQueue
	schedCfg *config.SchedulerConfig
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
	statusCounts, err := s.defRepo.CountListByStatus(req)
	if err != nil {
		return nil, fmt.Errorf("统计定时器状态失败: %w", err)
	}
	active := statusCounts[model.TimerStatusActive]
	inactive := statusCounts[model.TimerStatusInactive]

	return &model.TimerDefinitionListResponse{
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
		Items:    items,
		Stats: model.TimerDefinitionListStats{
			Total:    active + inactive,
			Active:   active,
			Inactive: inactive,
		},
	}, nil
}

// Activate 激活定时器
// 状态转换：INACTIVE -> ACTIVE
// 激活时立即同步未来 migrate_step_minutes*2 时间窗口内的任务到 Redis
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

	// 先发布 ACTIVE 状态，再让任何 PENDING 记录或 Redis 触发点对调度器可见。
	if err := s.defRepo.UpdateStatus(id, model.TimerStatusActive); err != nil {
		return fmt.Errorf("更新定时器激活状态失败: %w", err)
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

	// 只将本次真正创建成功的任务注册到分钟级动态分桶中。
	triggersByMinute := make(map[string][]*redis.TaskTrigger)
	for _, triggerTime := range triggerTimes {
		timeRange := triggerTime.Format("2006-01-02-15:04")

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

		triggersByMinute[timeRange] = append(triggersByMinute[timeRange], &redis.TaskTrigger{
			TimerID:     def.ID,
			TriggerTime: triggerTime,
		})
	}

	if err := pushMinuteTriggers(context.Background(), s.queue, s.schedCfg, triggersByMinute); err != nil {
		logger.Error("激活时同步任务到 Redis 失败", zap.Int64("timer_id", def.ID), zap.Error(err))
		return fmt.Errorf("同步任务到 Redis 失败: %w", err)
	}

	return nil
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

// GetRecord 根据 ID 获取单条执行记录。
func (s *TimerService) GetRecord(id int64) (*model.TimerRecord, error) {
	record, err := s.recRepo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("查询执行记录失败: %w", err)
	}
	if record == nil {
		return nil, fmt.Errorf("执行记录不存在，id=%d", id)
	}
	return record, nil
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
	statusCounts, err := s.recRepo.CountListByStatus(req)
	if err != nil {
		return nil, fmt.Errorf("统计执行记录状态失败: %w", err)
	}
	pending := statusCounts[model.RecordStatusPending]
	running := statusCounts[model.RecordStatusRunning]
	success := statusCounts[model.RecordStatusSuccess]
	failed := statusCounts[model.RecordStatusFailed]

	return &model.RecordListResponse{
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
		Items:    items,
		Stats: model.RecordListStats{
			Total:   pending + running + success + failed,
			Pending: pending,
			Running: running,
			Success: success,
			Failed:  failed,
		},
	}, nil
}
