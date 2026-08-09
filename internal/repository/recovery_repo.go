package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/chronoflow/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RecoveryResult struct {
	Reenqueued    int
	ExpiredLeases int
	FinalFailures int
}

type CleanupResult struct {
	OutboxEvents int64
	Executions   int64
}

type RecoveryRepository interface {
	RecoverBatch(
		ctx context.Context,
		now time.Time,
		staleBefore time.Time,
		limit int,
	) (RecoveryResult, error)
	Cleanup(
		ctx context.Context,
		outboxBefore time.Time,
		executionBefore time.Time,
		limit int,
	) (CleanupResult, error)
}

type recoveryRepo struct {
	db *gorm.DB
}

// NewRecoveryRepository 创建用于执行恢复和历史清理的仓库。
func NewRecoveryRepository(db *gorm.DB) RecoveryRepository {
	return &recoveryRepo{db: db}
}

// RecoverBatch 恢复超时、停滞或需要重新投递的 Execution。
func (r *recoveryRepo) RecoverBatch(
	ctx context.Context,
	now time.Time,
	staleBefore time.Time,
	limit int,
) (RecoveryResult, error) {
	var result RecoveryResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var executions []*model.TimerExecution
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where(
				"(status = ? AND scheduled_at <= ? AND "+
					"(last_enqueued_at IS NULL OR last_enqueued_at < ?)) OR "+
					"(status = ? AND next_attempt_at IS NOT NULL AND next_attempt_at <= ? AND "+
					"(last_enqueued_at IS NULL OR last_enqueued_at < ?)) OR "+
					"(status = ? AND lease_until IS NOT NULL AND lease_until < ?)",
				model.ExecutionStatusPending, now, staleBefore,
				model.ExecutionStatusRetryWait, now, staleBefore,
				model.ExecutionStatusRunning, now,
			).
			Order("scheduled_at ASC, id ASC").
			Limit(limit).
			Find(&executions).Error; err != nil {
			return err
		}

		for _, execution := range executions {
			if execution.Status == model.ExecutionStatusRunning {
				result.ExpiredLeases++
				if execution.Attempt >= execution.MaxAttempts {
					update := tx.Model(&model.TimerExecution{}).
						Where("id = ? AND status = ? AND run_token = ?",
							execution.ID,
							model.ExecutionStatusRunning,
							execution.RunToken,
						).
						Updates(map[string]any{
							"status":        model.ExecutionStatusFailed,
							"finished_at":   now,
							"error_message": "execution lease expired after reaching the maximum number of attempts",
							"lease_owner":   "",
							"lease_until":   nil,
							"run_token":     "",
						})
					if update.Error != nil {
						return update.Error
					}
					if update.RowsAffected == 1 {
						result.FinalFailures++
					}
					continue
				}
				if err := tx.Model(&model.TimerExecution{}).
					Where("id = ? AND status = ? AND run_token = ?",
						execution.ID,
						model.ExecutionStatusRunning,
						execution.RunToken,
					).
					Updates(map[string]any{
						"status":          model.ExecutionStatusRetryWait,
						"next_attempt_at": now,
						"lease_owner":     "",
						"lease_until":     nil,
						"run_token":       "",
						"error_message":   "execution lease expired and was re-enqueued by the reconciler",
					}).Error; err != nil {
					return err
				}
			}

			eventID := fmt.Sprintf(
				"execution-recovery-%d-%d",
				execution.ID,
				now.UnixNano(),
			)
			event, err := newExecutionOutboxEvent(
				execution.ID,
				eventID,
				model.OutboxEventExecutionRecovery,
				now,
				now,
			)
			if err != nil {
				return err
			}
			if err := tx.Create(event).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.TimerExecution{}).
				Where("id = ?", execution.ID).
				Update("last_enqueued_at", now).Error; err != nil {
				return err
			}
			result.Reenqueued++
		}
		return nil
	})
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("recover execution batch: %w", err)
	}
	return result, nil
}

// Cleanup 删除超过保留期限的已发布 Outbox 和终态 Execution。
func (r *recoveryRepo) Cleanup(
	ctx context.Context,
	outboxBefore time.Time,
	executionBefore time.Time,
	limit int,
) (CleanupResult, error) {
	var result CleanupResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		outbox := tx.Exec(
			"DELETE FROM outbox_events "+
				"WHERE published_at IS NOT NULL AND published_at < ? "+
				"ORDER BY id LIMIT ?",
			outboxBefore,
			limit,
		)
		if outbox.Error != nil {
			return outbox.Error
		}
		result.OutboxEvents = outbox.RowsAffected

		executions := tx.Exec(
			"DELETE FROM timer_executions "+
				"WHERE status IN (?, ?, ?) AND finished_at IS NOT NULL AND finished_at < ? "+
				"AND NOT EXISTS ("+
				"SELECT 1 FROM outbox_events WHERE aggregate_type = ? "+
				"AND aggregate_id = timer_executions.id"+
				") ORDER BY id LIMIT ?",
			model.ExecutionStatusSuccess,
			model.ExecutionStatusFailed,
			model.ExecutionStatusCancelled,
			executionBefore,
			model.OutboxAggregateTimerExecution,
			limit,
		)
		if executions.Error != nil {
			return executions.Error
		}
		result.Executions = executions.RowsAffected
		return nil
	})
	if err != nil {
		return CleanupResult{}, fmt.Errorf("clean up historical data: %w", err)
	}
	return result, nil
}
