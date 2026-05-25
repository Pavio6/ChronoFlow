package repository

import (
	"fmt"

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
	// GetActiveDefinitions 获取所有激活状态的定时器定义
	GetActiveDefinitions() ([]*model.TimerDefinition, error)
	// UpdateStatus 更新定时器定义状态
	UpdateStatus(id int64, status model.TimerStatus) error
	// IsActive 查询定时器当前是否处于可执行状态，不使用本地缓存
	IsActive(id int64) (bool, error)
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
		Update("status", model.TimerStatusDeleted)
	if result.Error != nil {
		return fmt.Errorf("删除定时器定义失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("定时器定义不存在或已被删除，id=%d", id)
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

// GetActiveDefinitions 获取所有激活状态的定时器定义
// 用于调度器加载需要执行的定时器
func (r *timerDefinitionRepo) GetActiveDefinitions() ([]*model.TimerDefinition, error) {
	var items []*model.TimerDefinition
	if err := r.db.Where("status = ?", model.TimerStatusActive).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("查询激活状态的定时器定义失败: %w", err)
	}
	return items, nil
}

// UpdateStatus 更新定时器定义状态
func (r *timerDefinitionRepo) UpdateStatus(id int64, status model.TimerStatus) error {
	result := r.db.Model(&model.TimerDefinition{}).
		Where("id = ?", id).
		Update("status", status)
	if result.Error != nil {
		return fmt.Errorf("更新定时器定义状态失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("定时器定义不存在，id=%d", id)
	}
	return nil
}

// IsActive 查询数据库中的实时状态，保证停用或删除后不执行已打点任务。
func (r *timerDefinitionRepo) IsActive(id int64) (bool, error) {
	var count int64
	if err := r.db.Model(&model.TimerDefinition{}).
		Where("id = ? AND status = ?", id, model.TimerStatusActive).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("查询定时器激活状态失败: %w", err)
	}
	return count > 0, nil
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
