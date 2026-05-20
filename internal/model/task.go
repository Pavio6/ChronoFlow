package model

import (
	"time"
)

// TaskStatus 任务状态类型
type TaskStatus string

// 任务状态常量
const (
	TaskStatusINIT     TaskStatus = "INIT"     // 初始化
	TaskStatusENABLED  TaskStatus = "ENABLED"  // 已启用
	TaskStatusDISABLED TaskStatus = "DISABLED" // 已禁用
	TaskStatusRUNNING  TaskStatus = "RUNNING"  // 运行中
	TaskStatusSUCCESS  TaskStatus = "SUCCESS"  // 执行成功
	TaskStatusFAILED   TaskStatus = "FAILED"   // 执行失败
	TaskStatusTIMEOUT  TaskStatus = "TIMEOUT"  // 执行超时
	TaskStatusDELETED  TaskStatus = "DELETED"  // 已删除
)

// Task 任务模型
type Task struct {
	ID              int64      `json:"id" gorm:"primaryKey;autoIncrement"`
	Name            string     `json:"name" gorm:"size:128;not null;comment:任务名称"`
	Description     string     `json:"description" gorm:"size:512;comment:任务描述"`
	CronExpr        string     `json:"cron_expr" gorm:"size:64;not null;comment:Cron表达式"`
	CallbackURL     string     `json:"callback_url" gorm:"size:512;not null;comment:回调URL"`
	CallbackMethod  string     `json:"callback_method" gorm:"size:16;not null;default:POST;comment:回调方法"`
	CallbackBody    string     `json:"callback_body" gorm:"type:text;comment:回调请求体"`
	CallbackHeaders string     `json:"callback_headers" gorm:"type:text;comment:回调请求头(JSON)"`
	Status          TaskStatus `json:"status" gorm:"size:32;not null;default:INIT;comment:任务状态"`
	Timeout         int        `json:"timeout" gorm:"default:30;comment:超时时间(秒)"`
	MaxRetries      int        `json:"max_retries" gorm:"default:3;comment:最大重试次数"`
	NextTriggerTime *time.Time `json:"next_trigger_time" gorm:"index;comment:下次触发时间"`
	LastTriggerTime *time.Time `json:"last_trigger_time" gorm:"comment:上次触发时间"`
	CreatedAt       time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName 指定表名
func (Task) TableName() string {
	return "tasks"
}

// IsRunnable 判断任务是否可运行
func (t *Task) IsRunnable() bool {
	return t.Status == TaskStatusENABLED
}

// IsDeleted 判断任务是否已删除
func (t *Task) IsDeleted() bool {
	return t.Status == TaskStatusDELETED
}

// CreateTaskRequest 创建任务请求
type CreateTaskRequest struct {
	Name            string            `json:"name" binding:"required,min=1,max=128"`
	Description     string            `json:"description" binding:"max=512"`
	CronExpr        string            `json:"cron_expr" binding:"required"`
	CallbackURL     string            `json:"callback_url" binding:"required,url"`
	CallbackMethod  string            `json:"callback_method" binding:"required,oneof=GET POST PUT DELETE PATCH"`
	CallbackBody    string            `json:"callback_body"`
	CallbackHeaders map[string]string `json:"callback_headers"`
	Timeout         int               `json:"timeout" binding:"min=1,max=300"`
	MaxRetries      int               `json:"max_retries" binding:"min=0,max=10"`
}

// UpdateTaskRequest 更新任务请求
type UpdateTaskRequest struct {
	Name            *string           `json:"name" binding:"omitempty,min=1,max=128"`
	Description     *string           `json:"description" binding:"omitempty,max=512"`
	CronExpr        *string           `json:"cron_expr"`
	CallbackURL     *string           `json:"callback_url" binding:"omitempty,url"`
	CallbackMethod  *string           `json:"callback_method" binding:"omitempty,oneof=GET POST PUT DELETE PATCH"`
	CallbackBody    *string           `json:"callback_body"`
	CallbackHeaders map[string]string `json:"callback_headers"`
	Timeout         *int              `json:"timeout" binding:"omitempty,min=1,max=300"`
	MaxRetries      *int              `json:"max_retries" binding:"omitempty,min=0,max=10"`
}

// TaskListRequest 任务列表请求
type TaskListRequest struct {
	Page     int        `form:"page" binding:"min=1"`
	PageSize int        `form:"page_size" binding:"min=1,max=100"`
	Status   TaskStatus `form:"status"`
	Keyword  string     `form:"keyword"`
}

// TaskListResponse 任务列表响应
type TaskListResponse struct {
	Total    int64   `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
	Tasks    []*Task `json:"tasks"`
}
