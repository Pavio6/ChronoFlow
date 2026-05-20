package repository

import (
	"fmt"
	"time"

	"github.com/chronoflow/internal/model"
	"gorm.io/gorm"
)

// ExecutionRepository 执行记录数据访问接口
type ExecutionRepository interface {
	Create(execution *model.TaskExecution) error
	GetByID(id int64) (*model.TaskExecution, error)
	Update(execution *model.TaskExecution) error
	List(req *model.ExecutionListRequest) ([]*model.TaskExecution, int64, error)
	GetByTaskID(taskID int64, limit int) ([]*model.TaskExecution, error)
	GetPendingRetries() ([]*model.TaskExecution, error)
	GetRunningExecutions(timeout time.Duration) ([]*model.TaskExecution, error)
	UpdateStatus(id int64, status model.ExecutionStatus) error
}

// executionRepository 执行记录数据访问实现
type executionRepository struct {
	db *gorm.DB
}

// NewExecutionRepository 创建执行记录仓库实例
func NewExecutionRepository(db *gorm.DB) ExecutionRepository {
	return &executionRepository{db: db}
}

// Create 创建执行记录
func (r *executionRepository) Create(execution *model.TaskExecution) error {
	return r.db.Create(execution).Error
}

// GetByID 根据 ID 获取执行记录
func (r *executionRepository) GetByID(id int64) (*model.TaskExecution, error) {
	var execution model.TaskExecution
	err := r.db.Where("id = ?", id).First(&execution).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("execution not found: %d", id)
		}
		return nil, err
	}
	return &execution, nil
}

// Update 更新执行记录
func (r *executionRepository) Update(execution *model.TaskExecution) error {
	return r.db.Save(execution).Error
}

// List 查询执行记录列表（分页）
func (r *executionRepository) List(req *model.ExecutionListRequest) ([]*model.TaskExecution, int64, error) {
	var executions []*model.TaskExecution
	var total int64

	query := r.db.Model(&model.TaskExecution{})

	// 按任务 ID 过滤
	if req.TaskID > 0 {
		query = query.Where("task_id = ?", req.TaskID)
	}

	// 按状态过滤
	if req.Status != "" {
		query = query.Where("status = ?", req.Status)
	}

	// 统计总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (req.Page - 1) * req.PageSize
	if err := query.Offset(offset).Limit(req.PageSize).
		Order("created_at DESC").
		Find(&executions).Error; err != nil {
		return nil, 0, err
	}

	return executions, total, nil
}

// GetByTaskID 根据任务 ID 获取执行记录
func (r *executionRepository) GetByTaskID(taskID int64, limit int) ([]*model.TaskExecution, error) {
	var executions []*model.TaskExecution
	err := r.db.Where("task_id = ?", taskID).
		Order("created_at DESC").
		Limit(limit).
		Find(&executions).Error
	return executions, err
}

// GetPendingRetries 获取待重试的执行记录
// 查询状态为 FAILED 且重试次数未超限，且已到达重试时间的记录
func (r *executionRepository) GetPendingRetries() ([]*model.TaskExecution, error) {
	var executions []*model.TaskExecution
	now := time.Now()

	err := r.db.Where("status = ? AND next_retry_time IS NOT NULL AND next_retry_time <= ?",
		model.ExecutionStatusFAILED, now).
		Find(&executions).Error

	return executions, err
}

// GetRunningExecutions 获取超时的运行中执行记录
func (r *executionRepository) GetRunningExecutions(timeout time.Duration) ([]*model.TaskExecution, error) {
	var executions []*model.TaskExecution
	threshold := time.Now().Add(-timeout)

	err := r.db.Where("status = ? AND started_at < ?",
		model.ExecutionStatusRUNNING, threshold).
		Find(&executions).Error

	return executions, err
}

// UpdateStatus 更新执行记录状态
func (r *executionRepository) UpdateStatus(id int64, status model.ExecutionStatus) error {
	return r.db.Model(&model.TaskExecution{}).
		Where("id = ?", id).
		Update("status", status).Error
}
