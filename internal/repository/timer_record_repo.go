package repository

import (
	"fmt"
	"time"

	"github.com/chronoflow/internal/model"
	"gorm.io/gorm"
)

// TimerRecordRepository 定时器执行记录仓库接口
// 提供对定时器执行记录表的 CRUD 操作及业务查询
type TimerRecordRepository interface {
	// Create 创建执行记录
	Create(record *model.TimerRecord) error
	// BatchCreate 批量创建执行记录
	BatchCreate(records []*model.TimerRecord) error
	// GetByID 根据 ID 获取执行记录
	GetByID(id int64) (*model.TimerRecord, error)
	// Update 更新执行记录
	Update(record *model.TimerRecord) error
	// List 分页查询执行记录列表
	List(req *model.RecordListRequest) ([]*model.TimerRecord, int64, error)
	// GetByTimerID 根据定时器 ID 获取最近的执行记录
	GetByTimerID(timerID int64, limit int) ([]*model.TimerRecord, error)
	// GetPendingRetries 获取待重试的执行记录（状态为 FAILED 且下次重试时间已到）
	GetPendingRetries() ([]*model.TimerRecord, error)
	// GetRunningRecords 获取超时的正在执行的记录（状态为 RUNNING 且执行时间超过指定时长）
	GetRunningRecords(timeout time.Duration) ([]*model.TimerRecord, error)
	// UpdateStatus 更新执行记录状态
	UpdateStatus(id int64, status model.RecordStatus) error
	// ExistsByTimerIDAndTriggerTime 幂等性检查：判断指定定时器在指定触发时间是否已有非 PENDING 状态的记录
	ExistsByTimerIDAndTriggerTime(timerID int64, triggerTime time.Time) (bool, error)
}

// timerRecordRepo 定时器执行记录仓库实现
type timerRecordRepo struct {
	db *gorm.DB
}

// NewTimerRecordRepository 创建定时器执行记录仓库实例
func NewTimerRecordRepository(db *gorm.DB) TimerRecordRepository {
	return &timerRecordRepo{db: db}
}

// Create 创建执行记录
func (r *timerRecordRepo) Create(record *model.TimerRecord) error {
	if err := r.db.Create(record).Error; err != nil {
		return fmt.Errorf("创建执行记录失败: %w", err)
	}
	return nil
}

// BatchCreate 批量创建执行记录
// 使用 GORM 的批量插入功能，每批 100 条
func (r *timerRecordRepo) BatchCreate(records []*model.TimerRecord) error {
	if len(records) == 0 {
		return nil
	}

	// 使用 GORM 的 CreateInBatches 分批插入，避免单次 SQL 过大
	if err := r.db.CreateInBatches(records, 100).Error; err != nil {
		return fmt.Errorf("批量创建执行记录失败: %w", err)
	}
	return nil
}

// GetByID 根据 ID 获取执行记录
func (r *timerRecordRepo) GetByID(id int64) (*model.TimerRecord, error) {
	var record model.TimerRecord
	err := r.db.Where("id = ?", id).First(&record).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("查询执行记录失败: %w", err)
	}
	return &record, nil
}

// Update 更新执行记录
func (r *timerRecordRepo) Update(record *model.TimerRecord) error {
	if err := r.db.Save(record).Error; err != nil {
		return fmt.Errorf("更新执行记录失败: %w", err)
	}
	return nil
}

// List 分页查询执行记录列表
// 支持按定时器 ID 和状态过滤
func (r *timerRecordRepo) List(req *model.RecordListRequest) ([]*model.TimerRecord, int64, error) {
	var items []*model.TimerRecord
	var total int64

	// 构建基础查询
	query := r.db.Model(&model.TimerRecord{})

	// 按定时器 ID 过滤
	if req.TimerID > 0 {
		query = query.Where("timer_id = ?", req.TimerID)
	}

	// 按状态过滤
	if req.Status != "" {
		query = query.Where("status = ?", req.Status)
	}

	// 统计满足条件的总记录数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计执行记录数量失败: %w", err)
	}

	// 计算分页偏移量
	offset := (req.Page - 1) * req.PageSize

	// 分页查询，按创建时间倒序排列
	if err := query.Order("created_at DESC").
		Offset(offset).
		Limit(req.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("查询执行记录列表失败: %w", err)
	}

	return items, total, nil
}

// GetByTimerID 根据定时器 ID 获取最近的执行记录
// 按触发时间倒序返回指定条数的记录
func (r *timerRecordRepo) GetByTimerID(timerID int64, limit int) ([]*model.TimerRecord, error) {
	var items []*model.TimerRecord
	err := r.db.Where("timer_id = ?", timerID).
		Order("trigger_time DESC").
		Limit(limit).
		Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("查询定时器执行记录失败: %w", err)
	}
	return items, nil
}

// GetPendingRetries 获取待重试的执行记录
// 查询条件：状态为 FAILED 且下次重试时间小于等于当前时间
func (r *timerRecordRepo) GetPendingRetries() ([]*model.TimerRecord, error) {
	var items []*model.TimerRecord
	now := time.Now()
	err := r.db.Where("status = ? AND next_retry_time <= ?", model.RecordStatusFailed, now).
		Order("next_retry_time ASC").
		Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("查询待重试记录失败: %w", err)
	}
	return items, nil
}

// GetRunningRecords 获取超时的正在执行的记录
// 查询条件：状态为 RUNNING 且开始执行时间早于当前时间减去超时时长
func (r *timerRecordRepo) GetRunningRecords(timeout time.Duration) ([]*model.TimerRecord, error) {
	var items []*model.TimerRecord
	threshold := time.Now().Add(-timeout)
	err := r.db.Where("status = ? AND started_at < ?", model.RecordStatusRunning, threshold).
		Order("started_at ASC").
		Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("查询超时执行记录失败: %w", err)
	}
	return items, nil
}

// UpdateStatus 更新执行记录状态
func (r *timerRecordRepo) UpdateStatus(id int64, status model.RecordStatus) error {
	result := r.db.Model(&model.TimerRecord{}).
		Where("id = ?", id).
		Update("status", status)
	if result.Error != nil {
		return fmt.Errorf("更新执行记录状态失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("执行记录不存在，id=%d", id)
	}
	return nil
}

// ExistsByTimerIDAndTriggerTime 幂等性检查
// 判断指定定时器在指定触发时间是否已有非 PENDING 状态的记录
// 用于防止重复创建执行记录
func (r *timerRecordRepo) ExistsByTimerIDAndTriggerTime(timerID int64, triggerTime time.Time) (bool, error) {
	var count int64
	err := r.db.Model(&model.TimerRecord{}).
		Where("timer_id = ? AND trigger_time = ? AND status != ?", timerID, triggerTime, model.RecordStatusPending).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("幂等性检查查询失败: %w", err)
	}
	return count > 0, nil
}
