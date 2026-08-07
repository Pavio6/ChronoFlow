package repository

import (
	"fmt"
	"time"

	"github.com/chronoflow/internal/model"
	"gorm.io/gorm"
)

// TimerDefinitionRepository 定时器定义仓库接口
// 提供对定时器定义表的创建、读取和状态管理操作
type TimerDefinitionRepository interface {
	// Create 创建定时器定义
	Create(def *model.TimerDefinition) error
	// GetByID 根据 ID 获取定时器定义
	GetByID(id int64) (*model.TimerDefinition, error)
	// Delete 逻辑删除定时器定义（将状态设置为 DELETED）
	Delete(id int64) error
	// List 分页查询定时器定义列表（支持应用名、状态、关键字过滤）
	List(req *model.TimerDefinitionListRequest) ([]*model.TimerDefinition, int64, error)
	// CountListByStatus 按列表筛选条件统计可见定时器状态
	CountListByStatus(req *model.TimerDefinitionListRequest) (map[model.TimerStatus]int64, error)
	// UpdateScheduleState conditionally changes status and next_fire_at.
	UpdateScheduleState(id int64, from, to model.TimerStatus, nextFireAt *time.Time) error
	// CountByStatus 按定时器状态统计数量
	CountByStatus() (map[model.TimerStatus]int64, error)
}

// timerDefinitionRepo 定时器定义仓库实现
type timerDefinitionRepo struct {
	db *gorm.DB
}

// NewTimerDefinitionRepository 创建定时器定义仓库实例
func NewTimerDefinitionRepository(db *gorm.DB) TimerDefinitionRepository {
	return &timerDefinitionRepo{db: db}
}

// Create 创建定时器定义
func (r *timerDefinitionRepo) Create(def *model.TimerDefinition) error {
	if err := r.db.Create(def).Error; err != nil {
		return fmt.Errorf("创建定时器定义失败: %w", err)
	}
	return nil
}

// GetByID 根据 ID 获取定时器定义
// 自动排除已逻辑删除的记录（status != DELETED）
func (r *timerDefinitionRepo) GetByID(id int64) (*model.TimerDefinition, error) {
	var def model.TimerDefinition
	err := r.db.Where("id = ? AND status != ?", id, model.TimerStatusDeleted).First(&def).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("查询定时器定义失败: %w", err)
	}
	return &def, nil
}

// Delete 逻辑删除定时器定义（将状态设置为 DELETED）
func (r *timerDefinitionRepo) Delete(id int64) error {
	result := r.db.Model(&model.TimerDefinition{}).
		Where("id = ? AND status != ?", id, model.TimerStatusDeleted).
		Updates(map[string]any{
			"status":       model.TimerStatusDeleted,
			"next_fire_at": nil,
			"version":      gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return fmt.Errorf("删除定时器定义失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("定时器定义不存在或已被删除，id=%d", id)
	}
	return nil
}

// UpdateScheduleState applies a state transition without allowing concurrent
// activation/deactivation requests to overwrite each other.
func (r *timerDefinitionRepo) UpdateScheduleState(
	id int64,
	from model.TimerStatus,
	to model.TimerStatus,
	nextFireAt *time.Time,
) error {
	result := r.db.Model(&model.TimerDefinition{}).
		Where("id = ? AND status = ?", id, from).
		Updates(map[string]any{
			"status":       to,
			"next_fire_at": nextFireAt,
			"version":      gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return fmt.Errorf("更新定时器调度状态失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("定时器状态已变化或不存在，id=%d, expected=%s", id, from)
	}
	return nil
}

// List 分页查询定时器定义列表
// 自动过滤已删除记录，支持按应用名、状态、关键字进行过滤
func (r *timerDefinitionRepo) List(req *model.TimerDefinitionListRequest) ([]*model.TimerDefinition, int64, error) {
	var items []*model.TimerDefinition
	var total int64

	// 构建基础查询，排除已删除记录
	query := r.db.Model(&model.TimerDefinition{}).Where("status != ?", model.TimerStatusDeleted)

	// 按应用名过滤
	if req.App != "" {
		query = query.Where("app = ?", req.App)
	}

	// 按状态过滤
	if req.Status != "" {
		query = query.Where("status = ?", req.Status)
	}

	// 按关键字模糊搜索（匹配名称或回调 URL）
	if req.Keyword != "" {
		keyword := "%" + req.Keyword + "%"
		query = query.Where("name LIKE ? OR callback_url LIKE ?", keyword, keyword)
	}

	// 统计满足条件的总记录数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计定时器定义数量失败: %w", err)
	}

	// 计算分页偏移量
	offset := (req.Page - 1) * req.PageSize

	// 分页查询，按创建时间倒序排列
	if err := query.Order("created_at DESC").
		Offset(offset).
		Limit(req.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("查询定时器定义列表失败: %w", err)
	}

	return items, total, nil
}

// CountListByStatus 按列表筛选条件统计未删除的定时器状态。
func (r *timerDefinitionRepo) CountListByStatus(req *model.TimerDefinitionListRequest) (map[model.TimerStatus]int64, error) {
	type row struct {
		Status model.TimerStatus
		Count  int64
	}

	query := r.db.Model(&model.TimerDefinition{}).Where("status != ?", model.TimerStatusDeleted)
	if req.App != "" {
		query = query.Where("app = ?", req.App)
	}
	if req.Status != "" {
		query = query.Where("status = ?", req.Status)
	}
	if req.Keyword != "" {
		keyword := "%" + req.Keyword + "%"
		query = query.Where("name LIKE ? OR callback_url LIKE ?", keyword, keyword)
	}

	var rows []row
	if err := query.Select("status, count(*) as count").Group("status").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("统计定时器列表状态失败: %w", err)
	}

	result := make(map[model.TimerStatus]int64, len(rows))
	for _, item := range rows {
		result[item.Status] = item.Count
	}
	return result, nil
}

// CountByStatus 按定时器状态统计数量
func (r *timerDefinitionRepo) CountByStatus() (map[model.TimerStatus]int64, error) {
	type row struct {
		Status model.TimerStatus
		Count  int64
	}

	var rows []row
	if err := r.db.Model(&model.TimerDefinition{}).
		Select("status, count(*) as count").
		Group("status").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("统计定时器状态失败: %w", err)
	}

	result := make(map[model.TimerStatus]int64, len(rows))
	for _, item := range rows {
		result[item.Status] = item.Count
	}
	return result, nil
}
