package pool

import (
	"testing"
	"time"
)

func TestSeparateWorkerPoolsIsolateBlockedExecution(t *testing.T) {
	schedulerPool, err := NewGoWorkerPool(1)
	if err != nil {
		t.Fatalf("NewGoWorkerPool for scheduler: %v", err)
	}
	defer schedulerPool.Release()

	triggerPool, err := NewGoWorkerPool(1)
	if err != nil {
		t.Fatalf("NewGoWorkerPool for trigger: %v", err)
	}
	defer triggerPool.Release()

	releaseExecution := make(chan struct{})
	startedExecution := make(chan struct{})
	if err := triggerPool.Submit(func() {
		close(startedExecution)
		<-releaseExecution
	}); err != nil {
		t.Fatalf("submit blocked execution task: %v", err)
	}
	<-startedExecution

	scheduled := make(chan struct{})
	if err := schedulerPool.Submit(func() { close(scheduled) }); err != nil {
		t.Fatalf("submit scheduler task: %v", err)
	}

	select {
	case <-scheduled:
	case <-time.After(time.Second):
		t.Fatal("scheduler task was blocked by occupied trigger pool")
	}
	close(releaseExecution)
}
