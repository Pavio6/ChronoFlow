package service

import (
	"testing"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/cron"
)

type stubDefinitionRepo struct {
	def            *model.TimerDefinition
	transitionFrom model.TimerStatus
	transitionTo   model.TimerStatus
	transitionNext *time.Time
}

// Create 为测试替身或测试辅助代码提供所需行为。
func (r *stubDefinitionRepo) Create(def *model.TimerDefinition) error {
	r.def = def
	if r.def.ID == 0 {
		r.def.ID = 1
	}
	return nil
}

// GetByID 为测试替身或测试辅助代码提供所需行为。
func (r *stubDefinitionRepo) GetByID(int64) (*model.TimerDefinition, error) {
	return r.def, nil
}

// Delete 为测试替身或测试辅助代码提供所需行为。
func (r *stubDefinitionRepo) Delete(int64) error { return nil }

// List 为测试替身或测试辅助代码提供所需行为。
func (r *stubDefinitionRepo) List(
	*model.TimerDefinitionListRequest,
) ([]*model.TimerDefinition, int64, error) {
	return nil, 0, nil
}

// CountListByStatus 为测试替身或测试辅助代码提供所需行为。
func (r *stubDefinitionRepo) CountListByStatus(
	*model.TimerDefinitionListRequest,
) (map[model.TimerStatus]int64, error) {
	return map[model.TimerStatus]int64{}, nil
}

// UpdateScheduleState 为测试替身或测试辅助代码提供所需行为。
func (r *stubDefinitionRepo) UpdateScheduleState(
	_ int64,
	from model.TimerStatus,
	to model.TimerStatus,
	next *time.Time,
) error {
	r.transitionFrom = from
	r.transitionTo = to
	r.transitionNext = next
	r.def.Status = to
	r.def.NextFireAt = next
	return nil
}

// CountByStatus 为测试替身或测试辅助代码提供所需行为。
func (r *stubDefinitionRepo) CountByStatus() (map[model.TimerStatus]int64, error) {
	return map[model.TimerStatus]int64{}, nil
}

// newTimerService 为测试替身或测试辅助代码提供所需行为。
func newTimerService(repo *stubDefinitionRepo) *TimerService {
	return NewTimerService(
		repo,
		cron.NewCronParser(),
		&config.SchedulerConfig{
			DefaultMaxCatchUp: 10,
		},
		&config.SecurityConfig{},
	)
}

// TestTimerServiceCreateDefaults 验证对应的测试场景。
func TestTimerServiceCreateDefaults(t *testing.T) {
	repo := &stubDefinitionRepo{}
	timerService := newTimerService(repo)

	definition, err := timerService.Create(&model.CreateTimerDefinitionRequest{
		App:            "app",
		Name:           "timer",
		CronExpr:       "0 * * * * *",
		CallbackURL:    "https://example.com/callback",
		CallbackMethod: "POST",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if definition.MisfirePolicy != model.MisfirePolicyFireOnce {
		t.Fatalf("misfire policy = %q, want FIRE_ONCE", definition.MisfirePolicy)
	}
	if definition.MaxCatchUp != 10 {
		t.Fatalf("max catch up = %d, want 10", definition.MaxCatchUp)
	}
}

// TestTimerServiceRejectsImpossibleCron 验证对应的测试场景。
func TestTimerServiceRejectsImpossibleCron(t *testing.T) {
	repo := &stubDefinitionRepo{}
	timerService := newTimerService(repo)

	_, err := timerService.Create(&model.CreateTimerDefinitionRequest{
		App:            "app",
		Name:           "timer",
		CronExpr:       "0 0 0 31 2 *",
		CallbackURL:    "https://example.com/callback",
		CallbackMethod: "POST",
	})
	if err == nil {
		t.Fatal("Create returned nil error for impossible cron")
	}
}

// TestTimerServiceActivationAndDeactivation 验证对应的测试场景。
func TestTimerServiceActivationAndDeactivation(t *testing.T) {
	repo := &stubDefinitionRepo{
		def: &model.TimerDefinition{
			ID:            7,
			CronExpr:      "0 * * * * *",
			Status:        model.TimerStatusInactive,
			MisfirePolicy: model.MisfirePolicyFireOnce,
		},
	}
	timerService := newTimerService(repo)

	if err := timerService.Activate(7); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if repo.transitionFrom != model.TimerStatusInactive || repo.transitionTo != model.TimerStatusActive {
		t.Fatalf("activation transition = %s -> %s", repo.transitionFrom, repo.transitionTo)
	}
	if repo.transitionNext == nil || !repo.transitionNext.After(time.Now()) {
		t.Fatalf("activation next_fire_at = %v, want future time", repo.transitionNext)
	}

	if err := timerService.Deactivate(7); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if repo.transitionFrom != model.TimerStatusActive || repo.transitionTo != model.TimerStatusInactive {
		t.Fatalf("deactivation transition = %s -> %s", repo.transitionFrom, repo.transitionTo)
	}
	if repo.transitionNext != nil {
		t.Fatalf("deactivation next_fire_at = %v, want nil", repo.transitionNext)
	}
}
