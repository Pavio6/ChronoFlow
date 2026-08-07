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

func (r *stubDefinitionRepo) Create(def *model.TimerDefinition) error {
	r.def = def
	if r.def.ID == 0 {
		r.def.ID = 1
	}
	return nil
}

func (r *stubDefinitionRepo) GetByID(int64) (*model.TimerDefinition, error) {
	return r.def, nil
}

func (r *stubDefinitionRepo) Delete(int64) error { return nil }

func (r *stubDefinitionRepo) List(
	*model.TimerDefinitionListRequest,
) ([]*model.TimerDefinition, int64, error) {
	return nil, 0, nil
}

func (r *stubDefinitionRepo) CountListByStatus(
	*model.TimerDefinitionListRequest,
) (map[model.TimerStatus]int64, error) {
	return map[model.TimerStatus]int64{}, nil
}

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

func (r *stubDefinitionRepo) CountByStatus() (map[model.TimerStatus]int64, error) {
	return map[model.TimerStatus]int64{}, nil
}

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
	if definition.Timezone != "UTC" {
		t.Fatalf("timezone = %q, want UTC", definition.Timezone)
	}
	if definition.MisfirePolicy != model.MisfirePolicyFireOnce {
		t.Fatalf("misfire policy = %q, want FIRE_ONCE", definition.MisfirePolicy)
	}
	if definition.MaxCatchUp != 10 {
		t.Fatalf("max catch up = %d, want 10", definition.MaxCatchUp)
	}
}

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

func TestTimerServiceActivationAndDeactivation(t *testing.T) {
	repo := &stubDefinitionRepo{
		def: &model.TimerDefinition{
			ID:            7,
			CronExpr:      "0 * * * * *",
			Status:        model.TimerStatusInactive,
			Timezone:      "UTC",
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
	if repo.transitionNext == nil || !repo.transitionNext.After(time.Now().UTC()) {
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
