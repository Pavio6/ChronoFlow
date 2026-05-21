package model

import (
	"time"
)

// ExecutionStatus 执行状态类型
type ExecutionStatus string

// 执行状态常量
const (
	ExecutionStatusPENDING  ExecutionStatus = "PENDING"  // 等待执行
	ExecutionStatusRUNNING  ExecutionStatus = "RUNNING"  // 执行中
	ExecutionStatusSUCCESS  ExecutionStatus = "SUCCESS"  // 执行成功
	ExecutionStatusFAILED   ExecutionStatus = "FAILED"   // 执行失败
	ExecutionStatusRETRYING ExecutionStatus = "RETRYING" // 重试中
	ExecutionStatusTIMEOUT  ExecutionStatus = "TIMEOUT"  // 执行超时
)

// TaskExecution 任务执行记录模型
type TaskExecution struct {
	ID             int64           `json:"id" gorm:"primaryKey;autoIncrement"`
	TaskID         int64           `json:"task_id" gorm:"index;not null;comment:任务ID"`
	TriggerTime    time.Time       `json:"trigger_time" gorm:"index;not null;comment:触发时间"`
	Status         ExecutionStatus `json:"status" gorm:"size:32;not null;default:PENDING;comment:执行状态"`
	RetryCount     int             `json:"retry_count" gorm:"default:0;comment:重试次数"`
	RequestURL     string          `json:"request_url" gorm:"size:512;comment:请求URL"`
	RequestMethod  string          `json:"request_method" gorm:"size:16;comment:请求方法"`
	RequestBody    string          `json:"request_body" gorm:"type:text;comment:请求体"`
	ResponseCode   int             `json:"response_code" gorm:"comment:响应状态码"`
	ResponseBody   string          `json:"response_body" gorm:"type:text;comment:响应体"`
	ErrorMessage   string          `json:"error_message" gorm:"type:text;comment:错误信息"`
	StartedAt      *time.Time      `json:"started_at" gorm:"comment:开始时间"`
	FinishedAt     *time.Time      `json:"finished_at" gorm:"comment:完成时间"`
	Duration       int64           `json:"duration" gorm:"comment:执行时长(毫秒)"`
	NextRetryTime  *time.Time      `json:"next_retry_time" gorm:"index;comment:下次重试时间"`
	CreatedAt      time.Time       `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt      time.Time       `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName 指定表名
func (TaskExecution) TableName() string {
	return "task_executions"
}

// IsCompleted 判断执行是否已完成
func (e *TaskExecution) IsCompleted() bool {
	return e.Status == ExecutionStatusSUCCESS ||
		e.Status == ExecutionStatusFAILED ||
		e.Status == ExecutionStatusTIMEOUT
}

// IsRetryable 判断是否可重试
func (e *TaskExecution) IsRetryable(maxRetries int) bool {
	return e.Status == ExecutionStatusFAILED &&
		e.RetryCount < maxRetries
}

// ExecutionListRequest 执行记录列表请求
type ExecutionListRequest struct {
	Page     int             `form:"page" binding:"min=1"`
	PageSize int             `form:"page_size" binding:"min=1,max=100"`
	TaskID   int64           `form:"task_id"`
	Status   ExecutionStatus `form:"status"`
}

// ExecutionListResponse 执行记录列表响应
type ExecutionListResponse struct {
	Total       int64            `json:"total"`
	Page        int              `json:"page"`
	PageSize    int              `json:"page_size"`
	Executions  []*TaskExecution `json:"executions"`
}
