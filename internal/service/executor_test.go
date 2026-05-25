package service

import (
	"context"
	"testing"
	"time"

	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/memory"
)

type stubTimerDefinitionRepo struct {
	def   *model.TimerDefinition
	calls int
}

func (r *stubTimerDefinitionRepo) Create(*model.TimerDefinition) error {
	return nil
}

func (r *stubTimerDefinitionRepo) GetByID(int64) (*model.TimerDefinition, error) {
	r.calls++
	if r.def == nil {
		return nil, nil
	}
	copy := *r.def
	return &copy, nil
}

func (r *stubTimerDefinitionRepo) Delete(int64) error {
	return nil
}

func (r *stubTimerDefinitionRepo) List(*model.TimerDefinitionListRequest) ([]*model.TimerDefinition, int64, error) {
	return nil, 0, nil
}

func (r *stubTimerDefinitionRepo) GetActiveDefinitions() ([]*model.TimerDefinition, error) {
	return nil, nil
}

func (r *stubTimerDefinitionRepo) UpdateStatus(int64, model.TimerStatus) error {
	return nil
}

func (r *stubTimerDefinitionRepo) CountByStatus() (map[model.TimerStatus]int64, error) {
	return nil, nil
}

func TestGetTimerDefinitionKeepsCachedActiveStatusWithinTTL(t *testing.T) {
	repo := &stubTimerDefinitionRepo{
		def: &model.TimerDefinition{ID: 1, Status: model.TimerStatusActive},
	}
	executor := &Executor{
		defRepo:  repo,
		cache:    memory.NewTimerCache(1),
		cacheTTL: 2 * time.Minute,
	}

	first := executor.getTimerDefinition(context.Background(), 1)
	if first == nil || first.Status != model.TimerStatusActive {
		t.Fatalf("first definition status = %v, want ACTIVE", first)
	}

	repo.def.Status = model.TimerStatusInactive
	second := executor.getTimerDefinition(context.Background(), 1)
	if second == nil || second.Status != model.TimerStatusActive {
		t.Fatalf("cached definition status = %v, want stale ACTIVE within TTL", second)
	}
	if repo.calls != 1 {
		t.Fatalf("repository calls = %d, want 1 while cache is valid", repo.calls)
	}
}

func TestGetTimerDefinitionDoesNotCacheInactiveStatus(t *testing.T) {
	repo := &stubTimerDefinitionRepo{
		def: &model.TimerDefinition{ID: 1, Status: model.TimerStatusInactive},
	}
	executor := &Executor{
		defRepo:  repo,
		cache:    memory.NewTimerCache(1),
		cacheTTL: 2 * time.Minute,
	}

	first := executor.getTimerDefinition(context.Background(), 1)
	if first == nil || first.Status != model.TimerStatusInactive {
		t.Fatalf("first definition status = %v, want INACTIVE", first)
	}

	repo.def.Status = model.TimerStatusActive
	second := executor.getTimerDefinition(context.Background(), 1)
	if second == nil || second.Status != model.TimerStatusActive {
		t.Fatalf("definition status after activation = %v, want ACTIVE", second)
	}
	if repo.calls != 2 {
		t.Fatalf("repository calls = %d, want 2 because INACTIVE is not cached", repo.calls)
	}
}
