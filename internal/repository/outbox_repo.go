package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/chronoflow/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OutboxRepository interface {
	ClaimBatch(
		ctx context.Context,
		owner string,
		now time.Time,
		limit int,
		claimTTL time.Duration,
	) ([]*model.OutboxEvent, error)
	MarkPublished(
		ctx context.Context,
		eventID string,
		owner string,
		messageID string,
		publishedAt time.Time,
	) error
	MarkFailed(
		ctx context.Context,
		eventID string,
		owner string,
		lastError string,
		nextAttemptAt time.Time,
	) error
	CountUnpublished(ctx context.Context) (int64, error)
}

type outboxRepo struct {
	db *gorm.DB
}

func NewOutboxRepository(db *gorm.DB) OutboxRepository {
	return &outboxRepo{db: db}
}

func (r *outboxRepo) ClaimBatch(
	ctx context.Context,
	owner string,
	now time.Time,
	limit int,
	claimTTL time.Duration,
) ([]*model.OutboxEvent, error) {
	var events []*model.OutboxEvent
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("published_at IS NULL").
			Where("available_at <= ?", now.UTC()).
			Where("(next_attempt_at IS NULL OR next_attempt_at <= ?)", now.UTC()).
			Where("(claim_until IS NULL OR claim_until < ? OR claim_owner = '')", now.UTC()).
			Order("id ASC").
			Limit(limit).
			Find(&events).Error; err != nil {
			return fmt.Errorf("领取 Outbox 事件失败: %w", err)
		}
		if len(events) == 0 {
			return nil
		}

		ids := make([]int64, 0, len(events))
		claimUntil := now.UTC().Add(claimTTL)
		for _, event := range events {
			ids = append(ids, event.ID)
			event.ClaimOwner = owner
			event.ClaimUntil = &claimUntil
		}
		update := tx.Model(&model.OutboxEvent{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"claim_owner": owner,
				"claim_until": claimUntil,
			})
		if update.Error != nil {
			return fmt.Errorf("更新 Outbox 领取状态失败: %w", update.Error)
		}
		if update.RowsAffected != int64(len(ids)) {
			return fmt.Errorf("Outbox 领取数量不一致: updated=%d, expected=%d", update.RowsAffected, len(ids))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (r *outboxRepo) MarkPublished(
	ctx context.Context,
	eventID string,
	owner string,
	messageID string,
	publishedAt time.Time,
) error {
	result := r.db.WithContext(ctx).
		Model(&model.OutboxEvent{}).
		Where("event_id = ? AND claim_owner = ? AND published_at IS NULL", eventID, owner).
		Updates(map[string]any{
			"published_at":         publishedAt.UTC(),
			"published_message_id": messageID,
			"attempts":             gorm.Expr("attempts + 1"),
			"claim_owner":          "",
			"claim_until":          nil,
			"last_error":           "",
		})
	if result.Error != nil {
		return fmt.Errorf("标记 Outbox 已发布失败: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("Outbox 发布确认失去所有权, event_id=%s", eventID)
	}
	return nil
}

func (r *outboxRepo) MarkFailed(
	ctx context.Context,
	eventID string,
	owner string,
	lastError string,
	nextAttemptAt time.Time,
) error {
	result := r.db.WithContext(ctx).
		Model(&model.OutboxEvent{}).
		Where("event_id = ? AND claim_owner = ? AND published_at IS NULL", eventID, owner).
		Updates(map[string]any{
			"attempts":        gorm.Expr("attempts + 1"),
			"next_attempt_at": nextAttemptAt.UTC(),
			"claim_owner":     "",
			"claim_until":     nil,
			"last_error":      lastError,
		})
	if result.Error != nil {
		return fmt.Errorf("记录 Outbox 发布失败状态失败: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("Outbox 失败确认失去所有权, event_id=%s", eventID)
	}
	return nil
}

func (r *outboxRepo) CountUnpublished(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).
		Model(&model.OutboxEvent{}).
		Where("published_at IS NULL").
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("统计未发布 Outbox 失败: %w", err)
	}
	return count, nil
}
