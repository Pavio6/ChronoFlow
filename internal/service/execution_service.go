package service

import (
	"fmt"

	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/repository"
)

// ExecutionService 执行记录服务接口
type ExecutionService interface {
	GetByID(id int64) (*model.TaskExecution, error)
	List(req *model.ExecutionListRequest) (*model.ExecutionListResponse, error)
	GetByTaskID(taskID int64, limit int) ([]*model.TaskExecution, error)
}

// executionService 执行记录服务实现
type executionService struct {
	execRepo repository.ExecutionRepository
}

// NewExecutionService 创建执行记录服务实例
func NewExecutionService(execRepo repository.ExecutionRepository) ExecutionService {
	return &executionService{
		execRepo: execRepo,
	}
}

// GetByID 根据 ID 获取执行记录
func (s *executionService) GetByID(id int64) (*model.TaskExecution, error) {
	return s.execRepo.GetByID(id)
}

// List 查询执行记录列表
func (s *executionService) List(req *model.ExecutionListRequest) (*model.ExecutionListResponse, error) {
	// 设置默认分页
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	executions, total, err := s.execRepo.List(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list executions: %w", err)
	}

	return &model.ExecutionListResponse{
		Total:      total,
		Page:       req.Page,
		PageSize:   req.PageSize,
		Executions: executions,
	}, nil
}

// GetByTaskID 根据任务 ID 获取执行记录
func (s *executionService) GetByTaskID(taskID int64, limit int) ([]*model.TaskExecution, error) {
	if limit <= 0 {
		limit = 10
	}
	return s.execRepo.GetByTaskID(taskID, limit)
}
