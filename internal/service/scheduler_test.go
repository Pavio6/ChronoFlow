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

func (r *stubDueTimerRepo) ScheduleDueBatch(
	context.Context,
	time.Time,
	int,
	repository.DueTimerResolver,
) (repository.ScheduleBatchResult, error) {
	return r.result, r.err
}

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

func TestSchedulerNormalOccurrence(t *testing.T) {
	scheduler := newTestScheduler()
	now := time.Date(2026, time.August, 7, 10, 0, 0, 500000000, time.UTC)
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

func TestSchedulerMisfirePolicies(t *testing.T) {
	scheduler := newTestScheduler()
	now := time.Date(2026, time.August, 7, 10, 5, 30, 0, time.UTC)
	due := time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)

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
			wantNext:        time.Date(2026, time.August, 7, 10, 6, 0, 0, time.UTC),
		},
		{
			name:            "fire once",
			policy:          model.MisfirePolicyFireOnce,
			wantOccurrences: 1,
			wantNext:        time.Date(2026, time.August, 7, 10, 6, 0, 0, time.UTC),
		},
		{
			name:            "catch up capped",
			policy:          model.MisfirePolicyCatchUp,
			maxCatchUp:      3,
			wantOccurrences: 3,
			wantNext:        time.Date(2026, time.August, 7, 10, 3, 0, 0, time.UTC),
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

func TestSchedulerUsesTimerTimezone(t *testing.T) {
	scheduler := newTestScheduler()
	dueUTC := time.Date(2026, time.August, 7, 2, 0, 0, 0, time.UTC)
	definition := testDefinition(dueUTC, model.MisfirePolicyFireOnce)
	definition.CronExpr = "0 0 10 * * *"
	definition.Timezone = "Asia/Shanghai"

	occurrences, next, err := scheduler.resolveTimer(definition, dueUTC)
	if err != nil {
		t.Fatalf("resolveTimer: %v", err)
	}
	if len(occurrences) != 1 || !occurrences[0].Equal(dueUTC) {
		t.Fatalf("occurrences = %v, want %s", occurrences, dueUTC)
	}
	wantNext := dueUTC.Add(24 * time.Hour)
	if !next.Equal(wantNext) {
		t.Fatalf("next = %s, want %s", next, wantNext)
	}
}

func testDefinition(next time.Time, policy model.MisfirePolicy) *model.TimerDefinition {
	return &model.TimerDefinition{
		ID:            1,
		CronExpr:      "0 * * * * *",
		Status:        model.TimerStatusActive,
		NextFireAt:    &next,
		Timezone:      "UTC",
		MisfirePolicy: policy,
		MaxCatchUp:    10,
		Version:       1,
	}
}
