package model

import "time"

const (
	OutboxAggregateTimerExecution = "timer_execution"
	OutboxEventExecutionReady     = "EXECUTION_READY"
	OutboxEventExecutionRetry     = "EXECUTION_RETRY"
	OutboxEventExecutionRecovery  = "EXECUTION_RECOVERY"
)

// OutboxEvent 与聚合变更一同提交，并由 Dispatcher 在之后发布。
type OutboxEvent struct {
	ID                 int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	EventID            string     `gorm:"size:64;not null;uniqueIndex:uk_outbox_event_id" json:"event_id"`
	AggregateType      string     `gorm:"size:64;not null" json:"aggregate_type"`
	AggregateID        int64      `gorm:"not null;index" json:"aggregate_id"`
	EventType          string     `gorm:"size:64;not null" json:"event_type"`
	Payload            string     `gorm:"type:json;not null" json:"payload"`
	AvailableAt        time.Time  `gorm:"not null;index:idx_outbox_publish,priority:2" json:"available_at"`
	PublishedAt        *time.Time `gorm:"index:idx_outbox_publish,priority:1" json:"published_at"`
	PublishedMessageID string     `gorm:"size:128" json:"published_message_id"`
	Attempts           int        `gorm:"type:int;not null;default:0" json:"attempts"`
	NextAttemptAt      *time.Time `gorm:"index:idx_outbox_publish,priority:3" json:"next_attempt_at"`
	ClaimOwner         string     `gorm:"size:128" json:"claim_owner"`
	ClaimUntil         *time.Time `gorm:"index:idx_outbox_publish,priority:4" json:"claim_until"`
	LastError          string     `gorm:"type:text" json:"last_error"`
	CreatedAt          time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 返回 Outbox 事件对应的数据表名称。
func (OutboxEvent) TableName() string {
	return "outbox_events"
}
