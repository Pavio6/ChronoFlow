package model

import "time"

// RecordStatus 执行记录状态
type RecordStatus string

const (
	// RecordStatusPending 等待执行
	RecordStatusPending RecordStatus = "PENDING"
	// RecordStatusRunning 执行中
	RecordStatusRunning RecordStatus = "RUNNING"
	// RecordStatusSuccess 执行成功
	RecordStatusSuccess RecordStatus = "SUCCESS"
	// RecordStatusFailed 执行失败
	RecordStatusFailed RecordStatus = "FAILED"
	// RecordStatusTimeout 执行超时
	RecordStatusTimeout RecordStatus = "TIMEOUT"
)

// TimerRecord 定时器执行记录
type TimerRecord struct {
	ID            int64        `gorm:"primaryKey;autoIncrement" json:"id"`
	TimerID       int64        `gorm:"index;not null;comment:定时器ID" json:"timer_id"`
	TriggerTime   time.Time    `gorm:"index;not null;comment:执行时间" json:"trigger_time"`
	Status        RecordStatus `gorm:"size:32;not null;default:PENDING;comment:执行状态" json:"status"`
	RequestURL    string       `gorm:"size:512;comment:请求URL" json:"request_url"`
	RequestMethod string       `gorm:"size:16;comment:请求方法" json:"request_method"`
	RequestBody   string       `gorm:"type:text;comment:请求体" json:"request_body"`
	ResponseCode  int          `gorm:"comment:响应状态码" json:"response_code"`
	ResponseBody  string       `gorm:"type:text;comment:响应体" json:"response_body"`
	ErrorMessage  string       `gorm:"type:text;comment:错误信息" json:"error_message"`
	StartedAt     *time.Time   `gorm:"comment:开始时间" json:"started_at"`
	FinishedAt    *time.Time   `gorm:"comment:完成时间" json:"finished_at"`
	Duration      int64        `gorm:"comment:执行时长(毫秒)" json:"duration"`
	CreatedAt     time.Time    `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time    `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 返回表名
func (TimerRecord) TableName() string {
	return "timer_records"
}

// IsCompleted 判断记录是否已完成（成功、失败或超时）
func (r *TimerRecord) IsCompleted() bool {
	return r.Status == RecordStatusSuccess ||
		r.Status == RecordStatusFailed ||
		r.Status == RecordStatusTimeout
}

// RecordListRequest 执行记录列表查询请求
type RecordListRequest struct {
	Page     int          `form:"page" binding:"min=1"`
	PageSize int          `form:"page_size" binding:"min=1,max=100"`
	TimerID  int64        `form:"timer_id"`
	Status   RecordStatus `form:"status"`
}

// RecordListResponse 执行记录列表查询响应
type RecordListResponse struct {
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Items    []*TimerRecord `json:"items"`
}
