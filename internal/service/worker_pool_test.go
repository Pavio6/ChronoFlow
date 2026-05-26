package service

import (
	"testing"

	"github.com/chronoflow/internal/config"
)

type stubWorkerPool struct {
	name string
}

func (*stubWorkerPool) Submit(func()) error {
	return nil
}

func TestSchedulerAndTriggerHoldTheirAssignedPools(t *testing.T) {
	schedulerPool := &stubWorkerPool{name: "scheduler"}
	triggerPool := &stubWorkerPool{name: "trigger"}
	cfg := &config.SchedulerConfig{}

	trigger := NewTrigger(nil, triggerPool, nil, nil, cfg)
	scheduler := NewScheduler(nil, schedulerPool, trigger, nil, cfg)

	if trigger.triggerPool != triggerPool {
		t.Fatal("Trigger did not retain its execution pool")
	}
	if scheduler.schedulerPool != schedulerPool {
		t.Fatal("Scheduler did not retain its scheduling pool")
	}
	if scheduler.schedulerPool == trigger.triggerPool {
		t.Fatal("Scheduler and Trigger unexpectedly share a worker pool")
	}
}
