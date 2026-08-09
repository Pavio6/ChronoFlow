package service

import (
	"context"
	"testing"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/cron"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/repository"
)

type stubDueTimerRepo struct {
	result repository.ScheduleBatchResult
	err    error
}

// ScheduleDueBatch 为测试替身或测试辅助代码提供所需行为。
func (r *stubDueTimerRepo) ScheduleDueBatch(
	context.Context,
	time.Time,
	int,
	repository.DueTimerResolver,
) (repository.ScheduleBatchResult, error) {
	return r.result, r.err
}

// newTestScheduler 为测试替身或测试辅助代码提供所需行为。
func newTestScheduler() *Scheduler {
	return NewScheduler(
		&stubDueTimerRepo{},
		cron.NewCronParser(),
		metrics.NewReporter(),
		&config.SchedulerConfig{
			BatchSize:           100,
			PollIntervalMS:      500,
			MisfireGraceSeconds: 5,
			DefaultMaxCatchUp:   10,
		},
	)
}

// TestSchedulerNormalOccurrence 验证对应的测试场景。
func TestSchedulerNormalOccurrence(t *testing.T) {
	scheduler := newTestScheduler()
	now := time.Date(2026, time.August, 7, 10, 0, 0, 500000000, time.Local)
	due := now.Truncate(time.Second)
	definition := testDefinition(due, model.MisfirePolicyFireOnce)

	occurrences, next, err := scheduler.resolveTimer(definition, now)
	if err != nil {
		t.Fatalf("resolveTimer: %v", err)
	}
	if len(occurrences) != 1 || !occurrences[0].Equal(due) {
		t.Fatalf("occurrences = %v, want [%s]", occurrences, due)
	}
	if !next.Equal(due.Add(time.Minute)) {
		t.Fatalf("next = %s, want %s", next, due.Add(time.Minute))
	}
}

// TestSchedulerMisfirePolicies 验证对应的测试场景。
func TestSchedulerMisfirePolicies(t *testing.T) {
	scheduler := newTestScheduler()
	now := time.Date(2026, time.August, 7, 10, 5, 30, 0, time.Local)
	due := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.Local)

	tests := []struct {
		name            string
		policy          model.MisfirePolicy
		maxCatchUp      int
		wantOccurrences int
		wantNext        time.Time
	}{
		{
			name:            "skip",
			policy:          model.MisfirePolicySkip,
			wantOccurrences: 0,
			wantNext:        time.Date(2026, time.August, 7, 10, 6, 0, 0, time.Local),
		},
		{
			name:            "fire once",
			policy:          model.MisfirePolicyFireOnce,
			wantOccurrences: 1,
			wantNext:        time.Date(2026, time.August, 7, 10, 6, 0, 0, time.Local),
		},
		{
			name:            "catch up capped",
			policy:          model.MisfirePolicyCatchUp,
			maxCatchUp:      3,
			wantOccurrences: 3,
			wantNext:        time.Date(2026, time.August, 7, 10, 3, 0, 0, time.Local),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition := testDefinition(due, tt.policy)
			definition.MaxCatchUp = tt.maxCatchUp
			occurrences, next, err := scheduler.resolveTimer(definition, now)
			if err != nil {
				t.Fatalf("resolveTimer: %v", err)
			}
			if len(occurrences) != tt.wantOccurrences {
				t.Fatalf("occurrence count = %d, want %d", len(occurrences), tt.wantOccurrences)
			}
			if !next.Equal(tt.wantNext) {
				t.Fatalf("next = %s, want %s", next, tt.wantNext)
			}
		})
	}
}

// testDefinition 为测试替身或测试辅助代码提供所需行为。
func testDefinition(next time.Time, policy model.MisfirePolicy) *model.TimerDefinition {
	return &model.TimerDefinition{
		ID:            1,
		CronExpr:      "0 * * * * *",
		Status:        model.TimerStatusActive,
		NextFireAt:    &next,
		MisfirePolicy: policy,
		MaxCatchUp:    10,
		Version:       1,
	}
}
