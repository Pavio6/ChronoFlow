package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/cron"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/pkg/logger"
	"go.uber.org/zap"
)

// TaskService 任务服务接口
type TaskService interface {
	Create(req *model.CreateTaskRequest) (*model.Task, error)
	GetByID(id int64) (*model.Task, error)
	Update(id int64, req *model.UpdateTaskRequest) (*model.Task, error)
	Delete(id int64) error
	List(req *model.TaskListRequest) (*model.TaskListResponse, error)
	Enable(id int64) error
	Disable(id int64) error
	Trigger(id int64) error
}

// taskService 任务服务实现
type taskService struct {
	taskRepo  repository.TaskRepository
	execRepo  repository.ExecutionRepository
	cronParser *cron.CronParser
}

// NewTaskService 创建任务服务实例
func NewTaskService(
	taskRepo repository.TaskRepository,
	execRepo repository.ExecutionRepository,
) TaskService {
	return &taskService{
		taskRepo:   taskRepo,
		execRepo:   execRepo,
		cronParser: cron.NewCronParser(),
	}
}

// Create 创建任务
func (s *taskService) Create(req *model.CreateTaskRequest) (*model.Task, error) {
	// 验证 Cron 表达式
	if err := cron.ValidateCronExpr(req.CronExpr); err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	// 计算下次触发时间
	nextTime, err := s.cronParser.NextTriggerTime(req.CronExpr, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to calculate next trigger time: %w", err)
	}

	// 序列化回调头
	headersJSON := "{}"
	if req.CallbackHeaders != nil {
		data, err := json.Marshal(req.CallbackHeaders)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal callback headers: %w", err)
		}
		headersJSON = string(data)
	}

	// 设置默认值
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30
	}
	maxRetries := req.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	task := &model.Task{
		Name:            req.Name,
		Description:     req.Description,
		CronExpr:        req.CronExpr,
		CallbackURL:     req.CallbackURL,
		CallbackMethod:  req.CallbackMethod,
		CallbackBody:    req.CallbackBody,
		CallbackHeaders: headersJSON,
		Status:          model.TaskStatusINIT,
		Timeout:         timeout,
		MaxRetries:      maxRetries,
		NextTriggerTime: &nextTime,
	}

	if err := s.taskRepo.Create(task); err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	logger.Info("task created",
		zap.Int64("task_id", task.ID),
		zap.String("name", task.Name),
	)

	return task, nil
}

// GetByID 根据 ID 获取任务
func (s *taskService) GetByID(id int64) (*model.Task, error) {
	return s.taskRepo.GetByID(id)
}

// Update 更新任务
func (s *taskService) Update(id int64, req *model.UpdateTaskRequest) (*model.Task, error) {
	task, err := s.taskRepo.GetByID(id)
	if err != nil {
		return nil, err
	}

	// 检查任务是否已删除
	if task.IsDeleted() {
		return nil, fmt.Errorf("cannot update deleted task")
	}

	// 更新字段
	if req.Name != nil {
		task.Name = *req.Name
	}
	if req.Description != nil {
		task.Description = *req.Description
	}
	if req.CronExpr != nil {
		// 验证新的 Cron 表达式
		if err := cron.ValidateCronExpr(*req.CronExpr); err != nil {
			return nil, fmt.Errorf("invalid cron expression: %w", err)
		}
		task.CronExpr = *req.CronExpr

		// 重新计算下次触发时间
		nextTime, err := s.cronParser.NextTriggerTime(task.CronExpr, time.Now())
		if err != nil {
			return nil, fmt.Errorf("failed to calculate next trigger time: %w", err)
		}
		task.NextTriggerTime = &nextTime
	}
	if req.CallbackURL != nil {
		task.CallbackURL = *req.CallbackURL
	}
	if req.CallbackMethod != nil {
		task.CallbackMethod = *req.CallbackMethod
	}
	if req.CallbackBody != nil {
		task.CallbackBody = *req.CallbackBody
	}
	if req.CallbackHeaders != nil {
		data, err := json.Marshal(req.CallbackHeaders)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal callback headers: %w", err)
		}
		task.CallbackHeaders = string(data)
	}
	if req.Timeout != nil {
		task.Timeout = *req.Timeout
	}
	if req.MaxRetries != nil {
		task.MaxRetries = *req.MaxRetries
	}

	if err := s.taskRepo.Update(task); err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	logger.Info("task updated",
		zap.Int64("task_id", task.ID),
		zap.String("name", task.Name),
	)

	return task, nil
}

// Delete 删除任务（软删除）
func (s *taskService) Delete(id int64) error {
	task, err := s.taskRepo.GetByID(id)
	if err != nil {
		return err
	}

	if task.IsDeleted() {
		return fmt.Errorf("task already deleted")
	}

	// 验证状态转换
	if !model.CanTransition(task.Status, model.TaskStatusDELETED) {
		return fmt.Errorf("cannot delete task in status: %s", task.Status)
	}

	if err := s.taskRepo.Delete(id); err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}

	logger.Info("task deleted", zap.Int64("task_id", id))
	return nil
}

// List 查询任务列表
func (s *taskService) List(req *model.TaskListRequest) (*model.TaskListResponse, error) {
	// 设置默认分页
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	tasks, total, err := s.taskRepo.List(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	return &model.TaskListResponse{
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
		Tasks:    tasks,
	}, nil
}

// Enable 启用任务
func (s *taskService) Enable(id int64) error {
	task, err := s.taskRepo.GetByID(id)
	if err != nil {
		return err
	}

	// 验证状态转换
	if !model.CanTransition(task.Status, model.TaskStatusENABLED) {
		return fmt.Errorf("cannot enable task in status: %s", task.Status)
	}

	// 计算下次触发时间
	nextTime, err := s.cronParser.NextTriggerTime(task.CronExpr, time.Now())
	if err != nil {
		return fmt.Errorf("failed to calculate next trigger time: %w", err)
	}

	// 更新状态和下次触发时间
	if err := s.taskRepo.UpdateStatus(id, model.TaskStatusENABLED); err != nil {
		return fmt.Errorf("failed to enable task: %w", err)
	}
	if err := s.taskRepo.UpdateNextTriggerTime(id, &nextTime); err != nil {
		return fmt.Errorf("failed to update next trigger time: %w", err)
	}

	logger.Info("task enabled",
		zap.Int64("task_id", id),
		zap.Time("next_trigger_time", nextTime),
	)

	return nil
}

// Disable 禁用任务
func (s *taskService) Disable(id int64) error {
	task, err := s.taskRepo.GetByID(id)
	if err != nil {
		return err
	}

	// 验证状态转换
	if !model.CanTransition(task.Status, model.TaskStatusDISABLED) {
		return fmt.Errorf("cannot disable task in status: %s", task.Status)
	}

	// 清除下次触发时间
	if err := s.taskRepo.UpdateNextTriggerTime(id, nil); err != nil {
		return fmt.Errorf("failed to clear next trigger time: %w", err)
	}

	if err := s.taskRepo.UpdateStatus(id, model.TaskStatusDISABLED); err != nil {
		return fmt.Errorf("failed to disable task: %w", err)
	}

	logger.Info("task disabled", zap.Int64("task_id", id))
	return nil
}

// Trigger 手动触发任务
func (s *taskService) Trigger(id int64) error {
	task, err := s.taskRepo.GetByID(id)
	if err != nil {
		return err
	}

	if !task.IsRunnable() {
		return fmt.Errorf("cannot trigger task in status: %s", task.Status)
	}

	// 创建执行记录
	execution := &model.TaskExecution{
		TaskID:      task.ID,
		TriggerTime: time.Now(),
		Status:      model.ExecutionStatusPENDING,
	}

	if err := s.execRepo.Create(execution); err != nil {
		return fmt.Errorf("failed to create execution: %w", err)
	}

	logger.Info("task manually triggered",
		zap.Int64("task_id", id),
		zap.Int64("execution_id", execution.ID),
	)

	return nil
}
