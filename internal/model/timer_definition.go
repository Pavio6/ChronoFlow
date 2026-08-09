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

// MisfirePolicy 定义 Scheduler 错过一个或多个触发点后的补偿方式。
type MisfirePolicy string

const (
	// MisfirePolicySkip 跳过所有已错过的触发点，只计算并保留未来的下一次触发时间。
	MisfirePolicySkip MisfirePolicy = "SKIP"
	// MisfirePolicyFireOnce 将已错过的触发点合并为一次立即补发，然后推进到未来的下一次触发时间。
	MisfirePolicyFireOnce MisfirePolicy = "FIRE_ONCE"
	// MisfirePolicyCatchUp 按时间顺序补发多个已错过的触发点，数量受 max_catch_up 限制。
	MisfirePolicyCatchUp MisfirePolicy = "CATCH_UP"
)

// TimerDefinition 定时器定义
type TimerDefinition struct {
	ID              int64         `gorm:"primaryKey;autoIncrement;index:idx_timer_due,priority:3" json:"id"`
	App             string        `gorm:"size:128;not null;comment:应用名" json:"app"`
	Name            string        `gorm:"size:128;not null;comment:定时器名称" json:"name"`
	CronExpr        string        `gorm:"size:64;not null;comment:Cron表达式" json:"cron_expr"`
	CallbackURL     string        `gorm:"size:512;not null;comment:回调URL" json:"callback_url"`
	CallbackMethod  string        `gorm:"size:16;not null;default:POST;comment:回调方法" json:"callback_method"`
	CallbackBody    string        `gorm:"type:text;comment:回调请求体" json:"callback_body"`
	CallbackHeaders string        `gorm:"type:text;comment:回调请求头(JSON)" json:"-"`
	Status          TimerStatus   `gorm:"size:32;not null;default:INACTIVE;index:idx_timer_due,priority:1;comment:状态" json:"status"`
	NextFireAt      *time.Time    `gorm:"index:idx_timer_due,priority:2;comment:下一次计划触发时间" json:"next_fire_at"`
	MisfirePolicy   MisfirePolicy `gorm:"size:32;not null;default:FIRE_ONCE;comment:错过触发策略" json:"misfire_policy"`
	MaxCatchUp      int           `gorm:"type:int;not null;default:10;comment:单轮最大补偿次数" json:"max_catch_up"`
	Version         int64         `gorm:"not null;default:1;comment:乐观锁版本" json:"version"`
	CreatedAt       time.Time     `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time     `gorm:"autoUpdateTime" json:"updated_at"`
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
	MisfirePolicy   MisfirePolicy     `json:"misfire_policy"`
	MaxCatchUp      int               `json:"max_catch_up" binding:"omitempty,min=1,max=1000"`
}

// TimerDefinitionListRequest 定时器列表查询请求
type TimerDefinitionListRequest struct {
	Page     int         `form:"page" binding:"min=1"`
	PageSize int         `form:"page_size" binding:"min=1,max=100"`
	App      string      `form:"app"`
	Status   TimerStatus `form:"status"`
	Keyword  string      `form:"keyword"`
}

// TimerDefinitionListResponse 定时器列表查询响应
type TimerDefinitionListResponse struct {
	Total    int64                    `json:"total"`
	Page     int                      `json:"page"`
	PageSize int                      `json:"page_size"`
	Items    []*TimerDefinition       `json:"items"`
	Stats    TimerDefinitionListStats `json:"stats"`
}

// TimerDefinitionListStats 定时器列表顶部使用的全量可见状态统计。
type TimerDefinitionListStats struct {
	Total    int64 `json:"total"`
	Active   int64 `json:"active"`
	Inactive int64 `json:"inactive"`
}
