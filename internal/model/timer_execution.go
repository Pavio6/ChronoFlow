package model

import "time"

// ExecutionStatus 表示一次计划触发对应 Execution 的持久化状态
type ExecutionStatus string

const (
	ExecutionStatusPending   ExecutionStatus = "PENDING"
	ExecutionStatusRunning   ExecutionStatus = "RUNNING"
	ExecutionStatusRetryWait ExecutionStatus = "RETRY_WAIT"
	ExecutionStatusSuccess   ExecutionStatus = "SUCCESS"
	ExecutionStatusFailed    ExecutionStatus = "FAILED"
	ExecutionStatusCancelled ExecutionStatus = "CANCELLED"
)

// TimerExecution 是一次计划回调的 MySQL 权威记录
type TimerExecution struct {
	ID              int64           `gorm:"primaryKey;autoIncrement" json:"id"`
	TimerID         int64           `gorm:"not null;uniqueIndex:uk_execution_schedule;index" json:"timer_id"`
	TimerName       string          `gorm:"column:timer_name;->;-:migration" json:"timer_name,omitempty"`
	ScheduledAt     time.Time       `gorm:"not null;uniqueIndex:uk_execution_schedule;index;comment:计划触发时间" json:"scheduled_at"`
	Status          ExecutionStatus `gorm:"size:32;not null;default:PENDING;index:idx_execution_recovery,priority:1" json:"status"`
	Attempt         int             `gorm:"type:int;not null;default:0" json:"attempt"`
	MaxAttempts     int             `gorm:"type:int;not null;default:3" json:"max_attempts"`
	NextAttemptAt   *time.Time      `gorm:"index:idx_execution_recovery,priority:3" json:"next_attempt_at"`
	LeaseOwner      string          `gorm:"size:128" json:"-"`
	LeaseUntil      *time.Time      `gorm:"index:idx_execution_recovery,priority:2" json:"-"`
	RunToken        string          `gorm:"size:64;index" json:"-"`
	LastEnqueuedAt  *time.Time      `gorm:"index:idx_execution_recovery,priority:4" json:"-"`
	RequestSnapshot string          `gorm:"type:json;not null;comment:回调配置快照" json:"-"`
	StartedAt       *time.Time      `json:"started_at"`
	FinishedAt      *time.Time      `json:"finished_at"`
	ResponseCode    int             `gorm:"type:int;not null;default:0" json:"response_code"`
	ResponseBody    string          `gorm:"type:text" json:"response_body"`
	ErrorMessage    string          `gorm:"type:text" json:"error_message"`
	DurationMS      int64           `json:"duration_ms"`
	CreatedAt       time.Time       `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time       `gorm:"autoUpdateTime" json:"updated_at"`
}

type ExecutionListRequest struct {
	Page      int             `form:"page" binding:"min=1"`
	PageSize  int             `form:"page_size" binding:"min=1,max=100"`
	TimerID   int64           `form:"timer_id"`
	TimerName string          `form:"timer_name"`
	Status    ExecutionStatus `form:"status"`
}

type ExecutionListResponse struct {
	Total    int64                     `json:"total"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
	Items    []*TimerExecution         `json:"items"`
	Stats    map[ExecutionStatus]int64 `json:"stats"`
}

// TableName 返回 Execution 对应的数据表名称
func (TimerExecution) TableName() string {
	return "timer_executions"
}

// CallbackSnapshot 固化调度时刻的回调配置
type CallbackSnapshot struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Body    string            `json:"body"`
	Headers map[string]string `json:"headers,omitempty"`
}

// IsTerminal 判断 Execution 是否已进入终态
func (e *TimerExecution) IsTerminal() bool {
	return e.Status == ExecutionStatusSuccess ||
		e.Status == ExecutionStatusFailed ||
		e.Status == ExecutionStatusCancelled
}
