package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chronoflow/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DueTimerResolver 为一条已锁定的 Timer 应用 Cron 和错过触发策略。
type DueTimerResolver func(
	definition *model.TimerDefinition,
	now time.Time,
) (occurrences []time.Time, nextFireAt time.Time, err error)

// ScheduleBatchResult 汇总一次已提交 Scheduler 事务的结果。
type ScheduleBatchResult struct {
	Timers     int
	Executions int
	Duplicates int
}

// DueTimerRepository 在一个事务中创建 Execution、推进 next_fire_at 并追加 Outbox 事件。
type DueTimerRepository interface {
	ScheduleDueBatch(
		ctx context.Context,
		now time.Time,
		batchSize int,
		resolve DueTimerResolver,
	) (ScheduleBatchResult, error)
}

type dueTimerRepo struct {
	db *gorm.DB
}

// NewDueTimerRepository 创建到期 Timer 调度仓库。
func NewDueTimerRepository(db *gorm.DB) DueTimerRepository {
	return &dueTimerRepo{db: db}
}

// ScheduleDueBatch 领取一批到期 Timer，并在事务中生成 Execution 与 Outbox 事件。
func (r *dueTimerRepo) ScheduleDueBatch(
	ctx context.Context,
	now time.Time,
	batchSize int,
	resolve DueTimerResolver,
) (ScheduleBatchResult, error) {
	var result ScheduleBatchResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var timers []*model.TimerDefinition
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND next_fire_at IS NOT NULL AND next_fire_at <= ?", model.TimerStatusActive, now).
			Order("next_fire_at ASC, id ASC").
			Limit(batchSize).
			Find(&timers).Error; err != nil {
			return fmt.Errorf("claim due timers: %w", err)
		}

		for _, definition := range timers {
			occurrences, nextFireAt, err := resolve(definition, now)
			if err != nil {
				return fmt.Errorf("resolve occurrences for timer %d: %w", definition.ID, err)
			}
			if nextFireAt.IsZero() {
				return fmt.Errorf("timer %d has an empty next_fire_at", definition.ID)
			}

			requestSnapshot, err := buildRequestSnapshot(definition)
			if err != nil {
				return fmt.Errorf("build request snapshot for timer %d: %w", definition.ID, err)
			}
			for _, scheduledAt := range occurrences {
				execution := &model.TimerExecution{
					TimerID:         definition.ID,
					ScheduledAt:     scheduledAt,
					Status:          model.ExecutionStatusPending,
					MaxAttempts:     3,
					LastEnqueuedAt:  timePointer(now),
					RequestSnapshot: requestSnapshot,
				}
				createResult := tx.Clauses(clause.OnConflict{
					Columns: []clause.Column{
						{Name: "timer_id"},
						{Name: "scheduled_at"},
					},
					DoNothing: true,
				}).Create(execution)
				if createResult.Error != nil {
					return fmt.Errorf("create execution: %w", createResult.Error)
				}
				if createResult.RowsAffected == 0 {
					result.Duplicates++
					continue
				}

				eventID := fmt.Sprintf("execution-ready-%d", execution.ID)
				payload, err := json.Marshal(map[string]any{
					"event_id":     eventID,
					"execution_id": execution.ID,
					"event_type":   model.OutboxEventExecutionReady,
					"created_at":   now.Format(time.RFC3339Nano),
				})
				if err != nil {
					return fmt.Errorf("marshal outbox event: %w", err)
				}
				event := &model.OutboxEvent{
					EventID:       eventID,
					AggregateType: model.OutboxAggregateTimerExecution,
					AggregateID:   execution.ID,
					EventType:     model.OutboxEventExecutionReady,
					Payload:       string(payload),
					AvailableAt:   now,
				}
				if err := tx.Create(event).Error; err != nil {
					return fmt.Errorf("create outbox event: %w", err)
				}
				result.Executions++
			}

			update := tx.Model(&model.TimerDefinition{}).
				Where("id = ? AND version = ?", definition.ID, definition.Version).
				Updates(map[string]any{
					"next_fire_at": nextFireAt,
					"version":      gorm.Expr("version + 1"),
				})
			if update.Error != nil {
				return fmt.Errorf("advance next_fire_at: %w", update.Error)
			}
			if update.RowsAffected != 1 {
				return fmt.Errorf("next_fire_at version conflict, timer_id=%d", definition.ID)
			}
			result.Timers++
		}
		return nil
	})
	if err != nil {
		return ScheduleBatchResult{}, err
	}
	return result, nil
}

// timePointer 返回指向给定时间值的指针。
func timePointer(value time.Time) *time.Time {
	return &value
}

// buildRequestSnapshot 从 Timer 定义构造不可变的回调请求快照。
func buildRequestSnapshot(definition *model.TimerDefinition) (string, error) {
	headers := make(map[string]string)
	if definition.CallbackHeaders != "" {
		if err := json.Unmarshal([]byte(definition.CallbackHeaders), &headers); err != nil {
			return "", err
		}
	}
	snapshot, err := json.Marshal(model.CallbackSnapshot{
		URL:     definition.CallbackURL,
		Method:  definition.CallbackMethod,
		Body:    definition.CallbackBody,
		Headers: headers,
	})
	if err != nil {
		return "", err
	}
	return string(snapshot), nil
}
