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

// DueTimerResolver applies Cron and misfire policy to one locked timer.
type DueTimerResolver func(
	definition *model.TimerDefinition,
	now time.Time,
) (occurrences []time.Time, nextFireAt time.Time, err error)

// ScheduleBatchResult summarizes one committed scheduler transaction.
type ScheduleBatchResult struct {
	Timers     int
	Executions int
	Duplicates int
}

// DueTimerRepository atomically creates executions, advances next_fire_at and
// appends Outbox events.
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

func NewDueTimerRepository(db *gorm.DB) DueTimerRepository {
	return &dueTimerRepo{db: db}
}

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
			Where("status = ? AND next_fire_at IS NOT NULL AND next_fire_at <= ?", model.TimerStatusActive, now.UTC()).
			Order("next_fire_at ASC, id ASC").
			Limit(batchSize).
			Find(&timers).Error; err != nil {
			return fmt.Errorf("领取到期定时器失败: %w", err)
		}

		for _, definition := range timers {
			occurrences, nextFireAt, err := resolve(definition, now)
			if err != nil {
				return fmt.Errorf("计算定时器 %d 触发点失败: %w", definition.ID, err)
			}
			if nextFireAt.IsZero() {
				return fmt.Errorf("定时器 %d 的 next_fire_at 不能为空", definition.ID)
			}

			requestSnapshot, err := buildRequestSnapshot(definition)
			if err != nil {
				return fmt.Errorf("构建定时器 %d 请求快照失败: %w", definition.ID, err)
			}
			for _, scheduledAt := range occurrences {
				execution := &model.TimerExecution{
					TimerID:         definition.ID,
					ScheduledAt:     scheduledAt.UTC(),
					Status:          model.ExecutionStatusPending,
					MaxAttempts:     3,
					LastEnqueuedAt:  timePointer(now.UTC()),
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
					return fmt.Errorf("创建执行记录失败: %w", createResult.Error)
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
					"created_at":   now.UTC().Format(time.RFC3339Nano),
				})
				if err != nil {
					return fmt.Errorf("序列化 Outbox 事件失败: %w", err)
				}
				event := &model.OutboxEvent{
					EventID:       eventID,
					AggregateType: model.OutboxAggregateTimerExecution,
					AggregateID:   execution.ID,
					EventType:     model.OutboxEventExecutionReady,
					Payload:       string(payload),
					AvailableAt:   now.UTC(),
				}
				if err := tx.Create(event).Error; err != nil {
					return fmt.Errorf("创建 Outbox 事件失败: %w", err)
				}
				result.Executions++
			}

			update := tx.Model(&model.TimerDefinition{}).
				Where("id = ? AND version = ?", definition.ID, definition.Version).
				Updates(map[string]any{
					"next_fire_at": nextFireAt.UTC(),
					"version":      gorm.Expr("version + 1"),
				})
			if update.Error != nil {
				return fmt.Errorf("推进 next_fire_at 失败: %w", update.Error)
			}
			if update.RowsAffected != 1 {
				return fmt.Errorf("推进 next_fire_at 发生版本冲突, timer_id=%d", definition.ID)
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

func timePointer(value time.Time) *time.Time {
	return &value
}

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
