package repository

import (
	"fmt"

	"github.com/chronoflow/internal/model"
	"gorm.io/gorm"
)

type ExecutionQueryRepository interface {
	GetExecutionByID(id int64) (*model.TimerExecution, error)
	ListExecutions(
		req *model.ExecutionListRequest,
	) ([]*model.TimerExecution, int64, map[model.ExecutionStatus]int64, error)
	GetExecutionsByTimerID(
		timerID int64,
		limit int,
	) ([]*model.TimerExecution, error)
}

type executionQueryRepo struct {
	db *gorm.DB
}

func NewExecutionQueryRepository(db *gorm.DB) ExecutionQueryRepository {
	return &executionQueryRepo{db: db}
}

func (r *executionQueryRepo) GetExecutionByID(
	id int64,
) (*model.TimerExecution, error) {
	var execution model.TimerExecution
	err := r.baseQuery().
		Where("timer_executions.id = ?", id).
		First(&execution).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询执行记录失败: %w", err)
	}
	return &execution, nil
}

func (r *executionQueryRepo) ListExecutions(
	req *model.ExecutionListRequest,
) ([]*model.TimerExecution, int64, map[model.ExecutionStatus]int64, error) {
	query := r.filteredQuery(req)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, nil, fmt.Errorf("统计执行记录失败: %w", err)
	}

	var items []*model.TimerExecution
	if err := query.
		Select("timer_executions.*, timer_definitions.name AS timer_name").
		Order("timer_executions.id DESC").
		Offset((req.Page - 1) * req.PageSize).
		Limit(req.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, nil, fmt.Errorf("查询执行记录列表失败: %w", err)
	}

	type row struct {
		Status model.ExecutionStatus
		Count  int64
	}
	var rows []row
	if err := r.filteredQuery(req).
		Select("timer_executions.status AS status, count(*) AS count").
		Group("timer_executions.status").
		Find(&rows).Error; err != nil {
		return nil, 0, nil, fmt.Errorf("统计执行状态失败: %w", err)
	}
	stats := make(map[model.ExecutionStatus]int64, len(rows))
	for _, item := range rows {
		stats[item.Status] = item.Count
	}
	return items, total, stats, nil
}

func (r *executionQueryRepo) GetExecutionsByTimerID(
	timerID int64,
	limit int,
) ([]*model.TimerExecution, error) {
	var items []*model.TimerExecution
	if err := r.baseQuery().
		Where("timer_executions.timer_id = ?", timerID).
		Order("timer_executions.scheduled_at DESC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("查询 Timer 执行记录失败: %w", err)
	}
	return items, nil
}

func (r *executionQueryRepo) baseQuery() *gorm.DB {
	return r.db.Model(&model.TimerExecution{}).
		Select("timer_executions.*, timer_definitions.name AS timer_name").
		Joins("LEFT JOIN timer_definitions ON timer_definitions.id = timer_executions.timer_id")
}

func (r *executionQueryRepo) filteredQuery(
	req *model.ExecutionListRequest,
) *gorm.DB {
	query := r.baseQuery()
	if req.TimerID > 0 {
		query = query.Where("timer_executions.timer_id = ?", req.TimerID)
	}
	if req.TimerName != "" {
		query = query.Where(
			"timer_definitions.name LIKE ?",
			"%"+req.TimerName+"%",
		)
	}
	if req.Status != "" {
		query = query.Where("timer_executions.status = ?", req.Status)
	}
	return query
}
