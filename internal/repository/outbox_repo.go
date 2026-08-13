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

// NewOutboxRepository 创建 Outbox 事件仓库
func NewOutboxRepository(db *gorm.DB) OutboxRepository {
	return &outboxRepo{db: db}
}

// ClaimBatch 在事务内领取一批可发布的 Outbox 事件并写入领取 Lease
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
			Where("available_at <= ?", now).
			Where("(next_attempt_at IS NULL OR next_attempt_at <= ?)", now).
			Where("(claim_until IS NULL OR claim_until < ? OR claim_owner = '')", now).
			Order("id ASC").
			Limit(limit).
			Find(&events).Error; err != nil {
			return fmt.Errorf("claim outbox events: %w", err)
		}
		if len(events) == 0 {
			return nil
		}

		ids := make([]int64, 0, len(events))
		claimUntil := now.Add(claimTTL)
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
			return fmt.Errorf("update outbox claim: %w", update.Error)
		}
		if update.RowsAffected != int64(len(ids)) {
			return fmt.Errorf("outbox claim count mismatch: updated=%d, expected=%d", update.RowsAffected, len(ids))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return events, nil
}

// MarkPublished 在仍持有领取 Lease 时记录事件已发布
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
			"published_at":         publishedAt,
			"published_message_id": messageID,
			"attempts":             gorm.Expr("attempts + 1"),
			"claim_owner":          "",
			"claim_until":          nil,
			"last_error":           "",
		})
	if result.Error != nil {
		return fmt.Errorf("mark outbox event as published: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("lost outbox publication ownership, event_id=%s", eventID)
	}
	return nil
}

// MarkFailed 在仍持有领取 Lease 时记录发布失败并安排下一次重试
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
			"next_attempt_at": nextAttemptAt,
			"claim_owner":     "",
			"claim_until":     nil,
			"last_error":      lastError,
		})
	if result.Error != nil {
		return fmt.Errorf("record outbox publication failure: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("lost outbox failure ownership, event_id=%s", eventID)
	}
	return nil
}

// CountUnpublished 返回尚未成功发布的 Outbox 事件数量
func (r *outboxRepo) CountUnpublished(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).
		Model(&model.OutboxEvent{}).
		Where("published_at IS NULL").
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count unpublished outbox events: %w", err)
	}
	return count, nil
}
