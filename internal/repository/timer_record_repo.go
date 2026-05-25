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
	// CountListByStatus 按列表筛选条件统计执行状态
	CountListByStatus(req *model.RecordListRequest) (map[model.RecordStatus]int64, error)
	// GetByTimerID 根据定时器 ID 获取最近的执行记录
	GetByTimerID(timerID int64, limit int) ([]*model.TimerRecord, error)
	// UpdateStatus 更新执行记录状态
	UpdateStatus(id int64, status model.RecordStatus) error
	// ClaimPendingByTimerIDAndTriggerTime 原子抢占一个待执行记录；仅 PENDING 可以进入 RUNNING
	ClaimPendingByTimerIDAndTriggerTime(timerID int64, triggerTime, startedAt time.Time) (*model.TimerRecord, bool, error)
	// HasStartedByTimerIDAndTriggerTime 判断执行记录是否已被领取或完成，供 Bloom 命中后确认
	HasStartedByTimerIDAndTriggerTime(timerID int64, triggerTime time.Time) (bool, error)
	// ExistsByTimerIDAndTriggerTime 幂等性检查：判断指定定时器在指定触发时间是否已有记录
	ExistsByTimerIDAndTriggerTime(timerID int64, triggerTime time.Time) (bool, error)
	// GetPendingByTimeRange 获取指定时间范围内的 PENDING 记录（Trigger DB 补偿用）
	GetPendingByTimeRange(start, end time.Time) ([]*model.TimerRecord, error)
	// HasPendingByTimeRangeAndBucket 判断指定时间范围和桶内是否存在 PENDING 记录
	HasPendingByTimeRangeAndBucket(start, end time.Time, bucket int, bucketNum int) (bool, error)
	// CountByStatus 按执行状态统计记录数量
	CountByStatus() (map[model.RecordStatus]int64, error)
	// CountPendingOverdue 统计触发时间已超过阈值但仍未执行的记录
	CountPendingOverdue(before time.Time) (int64, error)
	// CountRunningStale 统计开始时间已超过阈值但仍在运行的记录
	CountRunningStale(before time.Time) (int64, error)
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

// CountListByStatus 按列表筛选条件统计执行记录状态。
func (r *timerRecordRepo) CountListByStatus(req *model.RecordListRequest) (map[model.RecordStatus]int64, error) {
	type row struct {
		Status model.RecordStatus
		Count  int64
	}

	query := r.db.Model(&model.TimerRecord{})
	if req.TimerID > 0 {
		query = query.Where("timer_id = ?", req.TimerID)
	}
	if req.Status != "" {
		query = query.Where("status = ?", req.Status)
	}

	var rows []row
	if err := query.Select("status, count(*) as count").Group("status").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("统计执行记录列表状态失败: %w", err)
	}

	result := make(map[model.RecordStatus]int64, len(rows))
	for _, item := range rows {
		result[item.Status] = item.Count
	}
	return result, nil
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

// ClaimPendingByTimerIDAndTriggerTime 原子将唯一执行记录从 PENDING 转换为 RUNNING。
// 状态更新成功的执行器才有权触发外部回调。
func (r *timerRecordRepo) ClaimPendingByTimerIDAndTriggerTime(timerID int64, triggerTime, startedAt time.Time) (*model.TimerRecord, bool, error) {
	var record model.TimerRecord
	claimed := false

	err := r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.TimerRecord{}).
			Where("timer_id = ? AND trigger_time = ? AND status = ?", timerID, triggerTime, model.RecordStatusPending).
			Updates(map[string]interface{}{
				"status":     model.RecordStatusRunning,
				"started_at": startedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}

		claimed = true
		return tx.Where("timer_id = ? AND trigger_time = ?", timerID, triggerTime).First(&record).Error
	})
	if err != nil {
		return nil, false, fmt.Errorf("抢占待执行记录失败: %w", err)
	}
	if !claimed {
		return nil, false, nil
	}
	return &record, true, nil
}

// HasStartedByTimerIDAndTriggerTime 判断记录是否已离开 PENDING 状态。
// Bloom Filter 可能误判，因此命中后仍必须使用 MySQL 状态确认。
func (r *timerRecordRepo) HasStartedByTimerIDAndTriggerTime(timerID int64, triggerTime time.Time) (bool, error) {
	var count int64
	err := r.db.Model(&model.TimerRecord{}).
		Where("timer_id = ? AND trigger_time = ? AND status != ?", timerID, triggerTime, model.RecordStatusPending).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("查询执行状态失败: %w", err)
	}
	return count > 0, nil
}

// ExistsByTimerIDAndTriggerTime 幂等性检查
// 判断指定定时器在指定触发时间是否已有任意状态的记录，用于防止重复创建。
func (r *timerRecordRepo) ExistsByTimerIDAndTriggerTime(timerID int64, triggerTime time.Time) (bool, error) {
	var count int64
	err := r.db.Model(&model.TimerRecord{}).
		Where("timer_id = ? AND trigger_time = ?", timerID, triggerTime).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("幂等性检查查询失败: %w", err)
	}
	return count > 0, nil
}

// GetPendingByTimeRange 获取指定时间范围内的 PENDING 记录
// 用于 Trigger 的 DB 补偿机制：从 MySQL 查询未执行的任务，与 Redis 结果合并。
func (r *timerRecordRepo) GetPendingByTimeRange(start, end time.Time) ([]*model.TimerRecord, error) {
	var items []*model.TimerRecord
	err := r.db.Where("status = ? AND trigger_time >= ? AND trigger_time < ?", model.RecordStatusPending, start, end).
		Order("trigger_time ASC").
		Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("查询待执行记录失败: %w", err)
	}
	return items, nil
}

// HasPendingByTimeRangeAndBucket 判断指定时间范围和桶内是否存在 PENDING 记录
func (r *timerRecordRepo) HasPendingByTimeRangeAndBucket(start, end time.Time, bucket int, bucketNum int) (bool, error) {
	var count int64
	err := r.db.Model(&model.TimerRecord{}).
		Where("status = ? AND trigger_time >= ? AND trigger_time < ? AND MOD(timer_id, ?) = ?",
			model.RecordStatusPending, start, end, bucketNum, bucket).
		Limit(1).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("查询待执行记录失败: %w", err)
	}
	return count > 0, nil
}

// CountByStatus 按执行状态统计记录数量
func (r *timerRecordRepo) CountByStatus() (map[model.RecordStatus]int64, error) {
	type row struct {
		Status model.RecordStatus
		Count  int64
	}

	var rows []row
	if err := r.db.Model(&model.TimerRecord{}).
		Select("status, count(*) as count").
		Group("status").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("统计执行记录状态失败: %w", err)
	}

	result := make(map[model.RecordStatus]int64, len(rows))
	for _, item := range rows {
		result[item.Status] = item.Count
	}
	return result, nil
}

// CountPendingOverdue 统计触发时间在阈值之前的待执行记录。
func (r *timerRecordRepo) CountPendingOverdue(before time.Time) (int64, error) {
	var count int64
	if err := r.db.Model(&model.TimerRecord{}).
		Where("status = ? AND trigger_time < ?", model.RecordStatusPending, before).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("统计超期待执行记录失败: %w", err)
	}
	return count, nil
}

// CountRunningStale 统计开始时间在阈值之前的运行中记录。
func (r *timerRecordRepo) CountRunningStale(before time.Time) (int64, error) {
	var count int64
	if err := r.db.Model(&model.TimerRecord{}).
		Where("status = ? AND started_at IS NOT NULL AND started_at < ?", model.RecordStatusRunning, before).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("统计卡住执行记录失败: %w", err)
	}
	return count, nil
}
