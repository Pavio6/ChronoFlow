package service

import (
	"encoding/json"
	"fmt"

	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/cron"
	"github.com/chronoflow/internal/repository"
)

// TimerService 定时器业务逻辑层
// 封装定时器定义的 CRUD 操作和状态管理
type TimerService struct {
	defRepo repository.TimerDefinitionRepository
	recRepo repository.TimerRecordRepository
	parser  *cron.CronParser
}

// NewTimerService 创建定时器服务实例
func NewTimerService(
	defRepo repository.TimerDefinitionRepository,
	recRepo repository.TimerRecordRepository,
	parser *cron.CronParser,
) *TimerService {
	return &TimerService{
		defRepo: defRepo,
		recRepo: recRepo,
		parser:  parser,
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
		MaxRetries:      req.MaxRetries,
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
	if req.MaxRetries != nil {
		def.MaxRetries = *req.MaxRetries
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
