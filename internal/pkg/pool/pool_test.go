package pool

import (
	"context"
	"testing"
	"time"
)

func TestReleaseContextWaitsForRunningTask(t *testing.T) {
	workerPool, err := NewGoWorkerPool(1)
	if err != nil {
		t.Fatalf("NewGoWorkerPool: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	if err := workerPool.Submit(func() {
		close(started)
		<-release
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	<-started

	go func() {
		time.Sleep(10 * time.Millisecond)
		close(release)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := workerPool.ReleaseContext(ctx); err != nil {
		t.Fatalf("ReleaseContext: %v", err)
	}
}
