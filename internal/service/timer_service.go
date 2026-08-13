package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/callback"
	"github.com/chronoflow/internal/pkg/cron"
	"github.com/chronoflow/internal/repository"
)

// TimerService 定时器业务逻辑层
// 封装不可变定时器定义的创建、读取和状态管理
type TimerService struct {
	defRepo  repository.TimerDefinitionRepository
	parser   *cron.CronParser
	schedCfg *config.SchedulerConfig
	security *config.SecurityConfig
}

// NewTimerService 创建定时器服务实例
func NewTimerService(
	defRepo repository.TimerDefinitionRepository,
	parser *cron.CronParser,
	schedCfg *config.SchedulerConfig,
	securityCfg *config.SecurityConfig,
) *TimerService {
	return &TimerService{
		defRepo:  defRepo,
		parser:   parser,
		schedCfg: schedCfg,
		security: securityCfg,
	}
}

// Create 创建定时器定义
// 验证 Cron 表达式合法性后创建，默认状态为 INACTIVE
func (s *TimerService) Create(req *model.CreateTimerDefinitionRequest) (*model.TimerDefinition, error) {
	// 验证 Cron 表达式
	if err := s.parser.ValidateCronExpr(req.CronExpr); err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	if _, err := s.parser.NextTriggerTime(req.CronExpr, time.Now()); err != nil {
		return nil, fmt.Errorf("cron expression cannot produce a next trigger time: %w", err)
	}
	if err := callback.ValidateURL(
		req.CallbackURL,
		s.security.AllowPrivateCallbacks,
	); err != nil {
		return nil, err
	}
	misfirePolicy := req.MisfirePolicy
	if misfirePolicy == "" {
		misfirePolicy = model.MisfirePolicyFireOnce
	}
	switch misfirePolicy {
	case model.MisfirePolicySkip, model.MisfirePolicyFireOnce, model.MisfirePolicyCatchUp:
	default:
		return nil, fmt.Errorf("invalid misfire policy: %s", misfirePolicy)
	}
	maxCatchUp := req.MaxCatchUp
	if maxCatchUp < 1 {
		maxCatchUp = s.schedCfg.DefaultMaxCatchUp
	}

	// 序列化 callback_headers
	headersJSON := "{}"
	if req.CallbackHeaders != nil {
		b, err := json.Marshal(req.CallbackHeaders)
		if err != nil {
			return nil, fmt.Errorf("marshal callback headers: %w", err)
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
		MisfirePolicy:   misfirePolicy,
		MaxCatchUp:      maxCatchUp,
		Version:         1,
	}

	if err := s.defRepo.Create(def); err != nil {
		return nil, fmt.Errorf("create timer: %w", err)
	}

	return def, nil
}

// GetByID 根据 ID 获取定时器定义
func (s *TimerService) GetByID(id int64) (*model.TimerDefinition, error) {
	def, err := s.defRepo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("get timer: %w", err)
	}
	if def == nil {
		return nil, fmt.Errorf("timer does not exist, id=%d", id)
	}
	return def, nil
}

// Delete 逻辑删除定时器定义
func (s *TimerService) Delete(id int64) error {
	if err := s.defRepo.Delete(id); err != nil {
		return fmt.Errorf("delete timer: %w", err)
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
		return nil, fmt.Errorf("list timers: %w", err)
	}
	statusCounts, err := s.defRepo.CountListByStatus(req)
	if err != nil {
		return nil, fmt.Errorf("count timer statuses: %w", err)
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

// Activate 计算并保存权威的首次 next_fire_at，然后激活 Timer
func (s *TimerService) Activate(id int64) error {
	def, err := s.defRepo.GetByID(id)
	if err != nil {
		return fmt.Errorf("get timer: %w", err)
	}
	if def == nil {
		return fmt.Errorf("timer does not exist, id=%d", id)
	}

	// 验证状态转换合法性
	if err := model.ValidateTimerTransition(def.Status, model.TimerStatusActive); err != nil {
		return fmt.Errorf("activate timer: %w", err)
	}
	next, err := s.parser.NextTriggerTime(def.CronExpr, time.Now())
	if err != nil {
		return fmt.Errorf("calculate next trigger time: %w", err)
	}
	if err := s.defRepo.UpdateScheduleState(
		def.ID,
		model.TimerStatusInactive,
		model.TimerStatusActive,
		&next,
	); err != nil {
		return fmt.Errorf("activate timer state: %w", err)
	}
	return nil
}

// Deactivate 停用定时器
// 状态转换：ACTIVE -> INACTIVE
func (s *TimerService) Deactivate(id int64) error {
	def, err := s.defRepo.GetByID(id)
	if err != nil {
		return fmt.Errorf("get timer: %w", err)
	}
	if def == nil {
		return fmt.Errorf("timer does not exist, id=%d", id)
	}

	// 验证状态转换合法性
	if err := model.ValidateTimerTransition(def.Status, model.TimerStatusInactive); err != nil {
		return fmt.Errorf("deactivate timer: %w", err)
	}

	return s.defRepo.UpdateScheduleState(
		id,
		model.TimerStatusActive,
		model.TimerStatusInactive,
		nil,
	)
}
