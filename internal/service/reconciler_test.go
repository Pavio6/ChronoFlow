package service

import (
	"context"
	"testing"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/repository"
)

type stubRecoveryRepo struct {
	result        repository.RecoveryResult
	cleanupResult repository.CleanupResult
	recoverCalls  int
	cleanupCalls  int
}

func (r *stubRecoveryRepo) RecoverBatch(
	context.Context, time.Time, time.Time, int,
) (repository.RecoveryResult, error) {
	r.recoverCalls++
	return r.result, nil
}

func (r *stubRecoveryRepo) Cleanup(
	context.Context, time.Time, time.Time, int,
) (repository.CleanupResult, error) {
	r.cleanupCalls++
	return r.cleanupResult, nil
}

type stubReconcilerExecutionRepo struct {
	states map[model.ExecutionStatus]int64
}

func (r *stubReconcilerExecutionRepo) Claim(
	context.Context, int64, string, string, time.Time, time.Duration,
) (*model.TimerExecution, bool, error) {
	return nil, false, nil
}

func (r *stubReconcilerExecutionRepo) Heartbeat(
	context.Context, int64, string, string, time.Time,
) (bool, error) {
	return false, nil
}

func (r *stubReconcilerExecutionRepo) CompleteSuccess(
	context.Context, int64, string, string, time.Time, int, string, int64,
) (bool, error) {
	return false, nil
}

func (r *stubReconcilerExecutionRepo) CompleteFailure(
	context.Context,
	*model.TimerExecution,
	string,
	string,
	time.Time,
	int,
	string,
	string,
	int64,
	time.Time,
	bool,
) (bool, bool, error) {
	return false, false, nil
}

func (r *stubReconcilerExecutionRepo) CountByStatus(
	context.Context,
) (map[model.ExecutionStatus]int64, error) {
	return r.states, nil
}

func TestReconcilerRunsRecoveryAndCleanup(t *testing.T) {
	recoveryRepo := &stubRecoveryRepo{
		result: repository.RecoveryResult{
			Reenqueued:    2,
			ExpiredLeases: 1,
		},
		cleanupResult: repository.CleanupResult{
			OutboxEvents: 3,
			Executions:   4,
		},
	}
	executionRepo := &stubReconcilerExecutionRepo{
		states: map[model.ExecutionStatus]int64{
			model.ExecutionStatusPending: 2,
		},
	}
	reconciler := NewReconciler(
		recoveryRepo,
		executionRepo,
		metrics.NewReporter(),
		&config.RecoveryConfig{
			Enabled:                true,
			BatchSize:              100,
			PendingStaleSeconds:    30,
			OutboxRetentionDays:    7,
			ExecutionRetentionDays: 30,
		},
	)
	reconciler.now = func() time.Time {
		return time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)
	}

	reconciler.reconcile(context.Background())
	reconciler.cleanup(context.Background())

	if recoveryRepo.recoverCalls != 1 {
		t.Fatalf("recover calls = %d, want 1", recoveryRepo.recoverCalls)
	}
	if recoveryRepo.cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", recoveryRepo.cleanupCalls)
	}
}
