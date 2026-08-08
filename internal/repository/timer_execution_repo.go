package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chronoflow/internal/model"
	"gorm.io/gorm"
)

// TimerExecutionRepository owns the durable Worker state machine.
type TimerExecutionRepository interface {
	Claim(
		ctx context.Context,
		executionID int64,
		owner string,
		runToken string,
		now time.Time,
		leaseTTL time.Duration,
	) (*model.TimerExecution, bool, error)
	Heartbeat(
		ctx context.Context,
		executionID int64,
		owner string,
		runToken string,
		leaseUntil time.Time,
	) (bool, error)
	CompleteSuccess(
		ctx context.Context,
		executionID int64,
		owner string,
		runToken string,
		finishedAt time.Time,
		responseCode int,
		responseBody string,
		durationMS int64,
	) (bool, error)
	CompleteFailure(
		ctx context.Context,
		execution *model.TimerExecution,
		owner string,
		runToken string,
		finishedAt time.Time,
		responseCode int,
		responseBody string,
		errorMessage string,
		durationMS int64,
		retryAt time.Time,
		retryable bool,
	) (updated bool, retryScheduled bool, err error)
	CountByStatus(ctx context.Context) (map[model.ExecutionStatus]int64, error)
}

type timerExecutionRepo struct {
	db *gorm.DB
}

func NewTimerExecutionRepository(db *gorm.DB) TimerExecutionRepository {
	return &timerExecutionRepo{db: db}
}

func (r *timerExecutionRepo) Claim(
	ctx context.Context,
	executionID int64,
	owner string,
	runToken string,
	now time.Time,
	leaseTTL time.Duration,
) (*model.TimerExecution, bool, error) {
	leaseUntil := now.Add(leaseTTL)
	result := r.db.WithContext(ctx).
		Model(&model.TimerExecution{}).
		Where("id = ?", executionID).
		Where(
			"(status = ? AND scheduled_at <= ?) OR "+
				"(status = ? AND next_attempt_at IS NOT NULL AND next_attempt_at <= ?) OR "+
				"(status = ? AND lease_until IS NOT NULL AND lease_until < ?)",
			model.ExecutionStatusPending, now,
			model.ExecutionStatusRetryWait, now,
			model.ExecutionStatusRunning, now,
		).
		Updates(map[string]any{
			"status":        model.ExecutionStatusRunning,
			"attempt":       gorm.Expr("attempt + 1"),
			"lease_owner":   owner,
			"lease_until":   leaseUntil,
			"run_token":     runToken,
			"started_at":    now,
			"finished_at":   nil,
			"response_code": 0,
			"response_body": "",
			"error_message": "",
			"duration_ms":   0,
		})
	if result.Error != nil {
		return nil, false, fmt.Errorf("claim execution: %w", result.Error)
	}

	var execution model.TimerExecution
	if err := r.db.WithContext(ctx).First(&execution, executionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read execution: %w", err)
	}
	return &execution, result.RowsAffected == 1, nil
}

func (r *timerExecutionRepo) Heartbeat(
	ctx context.Context,
	executionID int64,
	owner string,
	runToken string,
	leaseUntil time.Time,
) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.TimerExecution{}).
		Where(
			"id = ? AND status = ? AND lease_owner = ? AND run_token = ?",
			executionID,
			model.ExecutionStatusRunning,
			owner,
			runToken,
		).
		Update("lease_until", leaseUntil)
	if result.Error != nil {
		return false, fmt.Errorf("renew execution lease: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

func (r *timerExecutionRepo) CompleteSuccess(
	ctx context.Context,
	executionID int64,
	owner string,
	runToken string,
	finishedAt time.Time,
	responseCode int,
	responseBody string,
	durationMS int64,
) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.TimerExecution{}).
		Where(
			"id = ? AND status = ? AND lease_owner = ? AND run_token = ?",
			executionID,
			model.ExecutionStatusRunning,
			owner,
			runToken,
		).
		Updates(map[string]any{
			"status":          model.ExecutionStatusSuccess,
			"finished_at":     finishedAt,
			"response_code":   responseCode,
			"response_body":   responseBody,
			"error_message":   "",
			"duration_ms":     durationMS,
			"lease_owner":     "",
			"lease_until":     nil,
			"run_token":       "",
			"next_attempt_at": nil,
		})
	if result.Error != nil {
		return false, fmt.Errorf("persist successful execution state: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

func (r *timerExecutionRepo) CompleteFailure(
	ctx context.Context,
	execution *model.TimerExecution,
	owner string,
	runToken string,
	finishedAt time.Time,
	responseCode int,
	responseBody string,
	errorMessage string,
	durationMS int64,
	retryAt time.Time,
	retryable bool,
) (bool, bool, error) {
	retryScheduled := retryable && execution.Attempt < execution.MaxAttempts
	updated := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"finished_at":   finishedAt,
			"response_code": responseCode,
			"response_body": responseBody,
			"error_message": errorMessage,
			"duration_ms":   durationMS,
			"lease_owner":   "",
			"lease_until":   nil,
			"run_token":     "",
		}
		if retryScheduled {
			updates["status"] = model.ExecutionStatusRetryWait
			updates["next_attempt_at"] = retryAt
			updates["last_enqueued_at"] = retryAt
		} else {
			updates["status"] = model.ExecutionStatusFailed
			updates["next_attempt_at"] = nil
		}

		result := tx.Model(&model.TimerExecution{}).
			Where(
				"id = ? AND status = ? AND lease_owner = ? AND run_token = ?",
				execution.ID,
				model.ExecutionStatusRunning,
				owner,
				runToken,
			).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		updated = true
		if !retryScheduled {
			return nil
		}

		event, err := newExecutionOutboxEvent(
			execution.ID,
			fmt.Sprintf("execution-retry-%d-%d", execution.ID, execution.Attempt),
			model.OutboxEventExecutionRetry,
			retryAt,
			finishedAt,
		)
		if err != nil {
			return err
		}
		return tx.Create(event).Error
	})
	if err != nil {
		return false, false, fmt.Errorf("persist failed execution state: %w", err)
	}
	return updated, updated && retryScheduled, nil
}

func (r *timerExecutionRepo) CountByStatus(
	ctx context.Context,
) (map[model.ExecutionStatus]int64, error) {
	type row struct {
		Status model.ExecutionStatus
		Count  int64
	}
	var rows []row
	if err := r.db.WithContext(ctx).
		Model(&model.TimerExecution{}).
		Select("status, count(*) AS count").
		Group("status").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("count execution statuses: %w", err)
	}
	result := make(map[model.ExecutionStatus]int64, len(rows))
	for _, item := range rows {
		result[item.Status] = item.Count
	}
	return result, nil
}

func newExecutionOutboxEvent(
	executionID int64,
	eventID string,
	eventType string,
	availableAt time.Time,
	createdAt time.Time,
) (*model.OutboxEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"event_id":     eventID,
		"execution_id": executionID,
		"event_type":   eventType,
		"created_at":   createdAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal execution outbox event: %w", err)
	}
	return &model.OutboxEvent{
		EventID:       eventID,
		AggregateType: model.OutboxAggregateTimerExecution,
		AggregateID:   executionID,
		EventType:     eventType,
		Payload:       string(payload),
		AvailableAt:   availableAt,
	}, nil
}
