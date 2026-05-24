package model

import "time"

// TimerStatus 定时器状态
type TimerStatus string

const (
	// TimerStatusActive 激活状态，定时器正常运行
	TimerStatusActive TimerStatus = "ACTIVE"
	// TimerStatusInactive 未激活状态，定时器暂停
	TimerStatusInactive TimerStatus = "INACTIVE"
	// TimerStatusDeleted 已删除状态，定时器被逻辑删除
	TimerStatusDeleted TimerStatus = "DELETED"
)

// TimerDefinition 定时器定义
type TimerDefinition struct {
	ID              int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	App             string      `gorm:"size:128;not null;comment:应用名" json:"app"`
	Name            string      `gorm:"size:128;not null;comment:定时器名称" json:"name"`
	CronExpr        string      `gorm:"size:64;not null;comment:Cron表达式" json:"cron_expr"`
	CallbackURL     string      `gorm:"size:512;not null;comment:回调URL" json:"callback_url"`
	CallbackMethod  string      `gorm:"size:16;not null;default:POST;comment:回调方法" json:"callback_method"`
	CallbackBody    string      `gorm:"type:text;comment:回调请求体" json:"callback_body"`
	CallbackHeaders string      `gorm:"type:text;comment:回调请求头(JSON)" json:"callback_headers"`
	Status          TimerStatus `gorm:"size:32;not null;default:INACTIVE;comment:状态" json:"status"`
	Timeout         int         `gorm:"default:30;comment:超时时间(秒)" json:"timeout"`
	MaxRetries      int         `gorm:"default:3;comment:最大重试次数" json:"max_retries"`
	CreatedAt       time.Time   `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time   `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 返回表名
func (TimerDefinition) TableName() string {
	return "timer_definitions"
}

// CreateTimerDefinitionRequest 创建定时器请求
type CreateTimerDefinitionRequest struct {
	App             string            `json:"app" binding:"required"`
	Name            string            `json:"name" binding:"required,min=1,max=128"`
	CronExpr        string            `json:"cron_expr" binding:"required"`
	CallbackURL     string            `json:"callback_url" binding:"required,url"`
	CallbackMethod  string            `json:"callback_method" binding:"required,oneof=GET POST PUT DELETE PATCH"`
	CallbackBody    string            `json:"callback_body"`
	CallbackHeaders map[string]string `json:"callback_headers"`
	Timeout         int               `json:"timeout" binding:"min=1,max=300"`
	MaxRetries      int               `json:"max_retries" binding:"min=0,max=10"`
}

// UpdateTimerDefinitionRequest 更新定时器请求（使用指针字段实现部分更新）
type UpdateTimerDefinitionRequest struct {
	Name            *string            `json:"name" binding:"omitempty,min=1,max=128"`
	CronExpr        *string            `json:"cron_expr" binding:"omitempty"`
	CallbackURL     *string            `json:"callback_url" binding:"omitempty,url"`
	CallbackMethod  *string            `json:"callback_method" binding:"omitempty,oneof=GET POST PUT DELETE PATCH"`
	CallbackBody    *string            `json:"callback_body"`
	CallbackHeaders *map[string]string `json:"callback_headers"`
	Timeout         *int               `json:"timeout" binding:"omitempty,min=1,max=300"`
	MaxRetries      *int               `json:"max_retries" binding:"omitempty,min=0,max=10"`
}

// TimerDefinitionListRequest 定时器列表查询请求
type TimerDefinitionListRequest struct {
	Page     int          `form:"page" binding:"min=1"`
	PageSize int          `form:"page_size" binding:"min=1,max=100"`
	App      string       `form:"app"`
	Status   TimerStatus  `form:"status"`
	Keyword  string       `form:"keyword"`
}

// TimerDefinitionListResponse 定时器列表查询响应
type TimerDefinitionListResponse struct {
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
	Items    []*TimerDefinition  `json:"items"`
}
