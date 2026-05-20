package repository

import (
	"fmt"
	"time"

	"github.com/chronoflow/internal/model"
	"gorm.io/gorm"
)

// TaskRepository 任务数据访问接口
type TaskRepository interface {
	Create(task *model.Task) error
	GetByID(id int64) (*model.Task, error)
	Update(task *model.Task) error
	Delete(id int64) error
	List(req *model.TaskListRequest) ([]*model.Task, int64, error)
	GetEnabledTasks() ([]*model.Task, error)
	GetTasksToSchedule(limit int) ([]*model.Task, error)
	UpdateNextTriggerTime(id int64, nextTime *time.Time) error
	UpdateStatus(id int64, status model.TaskStatus) error
}

// taskRepository 任务数据访问实现
type taskRepository struct {
	db *gorm.DB
}

// NewTaskRepository 创建任务仓库实例
func NewTaskRepository(db *gorm.DB) TaskRepository {
	return &taskRepository{db: db}
}

// Create 创建任务
func (r *taskRepository) Create(task *model.Task) error {
	return r.db.Create(task).Error
}

// GetByID 根据 ID 获取任务
func (r *taskRepository) GetByID(id int64) (*model.Task, error) {
	var task model.Task
	err := r.db.Where("id = ?", id).First(&task).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("task not found: %d", id)
		}
		return nil, err
	}
	return &task, nil
}

// Update 更新任务
func (r *taskRepository) Update(task *model.Task) error {
	return r.db.Save(task).Error
}

// Delete 删除任务（软删除，标记为 DELETED 状态）
func (r *taskRepository) Delete(id int64) error {
	return r.db.Model(&model.Task{}).
		Where("id = ?", id).
		Update("status", model.TaskStatusDELETED).Error
}

// List 查询任务列表（分页）
func (r *taskRepository) List(req *model.TaskListRequest) ([]*model.Task, int64, error) {
	var tasks []*model.Task
	var total int64

	query := r.db.Model(&model.Task{})

	// 过滤已删除的任务
	query = query.Where("status != ?", model.TaskStatusDELETED)

	// 按状态过滤
	if req.Status != "" {
		query = query.Where("status = ?", req.Status)
	}

	// 按关键词搜索
	if req.Keyword != "" {
		query = query.Where("name LIKE ? OR description LIKE ?",
			"%"+req.Keyword+"%", "%"+req.Keyword+"%")
	}

	// 统计总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (req.Page - 1) * req.PageSize
	if err := query.Offset(offset).Limit(req.PageSize).
		Order("created_at DESC").
		Find(&tasks).Error; err != nil {
		return nil, 0, err
	}

	return tasks, total, nil
}

// GetEnabledTasks 获取所有已启用的任务
func (r *taskRepository) GetEnabledTasks() ([]*model.Task, error) {
	var tasks []*model.Task
	err := r.db.Where("status = ?", model.TaskStatusENABLED).
		Find(&tasks).Error
	return tasks, err
}

// GetTasksToSchedule 获取需要调度的任务（已启用且未设置下次触发时间或已到达触发时间）
func (r *taskRepository) GetTasksToSchedule(limit int) ([]*model.Task, error) {
	var tasks []*model.Task
	now := time.Now()

	err := r.db.Where("status = ? AND (next_trigger_time IS NULL OR next_trigger_time <= ?)",
		model.TaskStatusENABLED, now).
		Limit(limit).
		Find(&tasks).Error

	return tasks, err
}

// UpdateNextTriggerTime 更新任务的下次触发时间
func (r *taskRepository) UpdateNextTriggerTime(id int64, nextTime *time.Time) error {
	return r.db.Model(&model.Task{}).
		Where("id = ?", id).
		Update("next_trigger_time", nextTime).Error
}

// UpdateStatus 更新任务状态
func (r *taskRepository) UpdateStatus(id int64, status model.TaskStatus) error {
	return r.db.Model(&model.Task{}).
		Where("id = ?", id).
		Update("status", status).Error
}
