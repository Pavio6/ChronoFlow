package memory

import (
	"context"
	"testing"
	"time"

	"github.com/chronoflow/internal/model"
)

func TestGetDeletesExpiredEntry(t *testing.T) {
	cache := NewTimerCache(2)
	cache.Set(1, &model.TimerDefinition{ID: 1}, -time.Second)

	if _, ok := cache.Get(1); ok {
		t.Fatal("expired entry returned as a cache hit")
	}
	if got := cache.Size(); got != 0 {
		t.Fatalf("cache size = %d, want 0 after expired entry is read", got)
	}
}

func TestSetEvictsEarliestExpiringEntryWhenFull(t *testing.T) {
	cache := NewTimerCache(2)
	cache.Set(1, &model.TimerDefinition{ID: 1}, time.Minute)
	cache.Set(2, &model.TimerDefinition{ID: 2}, 2*time.Minute)
	cache.Set(3, &model.TimerDefinition{ID: 3}, 3*time.Minute)

	if got := cache.Size(); got != 2 {
		t.Fatalf("cache size = %d, want 2", got)
	}
	if _, ok := cache.Get(1); ok {
		t.Fatal("earliest expiring entry was not evicted")
	}
	if _, ok := cache.Get(2); !ok {
		t.Fatal("entry 2 should remain cached")
	}
	if _, ok := cache.Get(3); !ok {
		t.Fatal("entry 3 should remain cached")
	}
}

func TestStartCleanupRemovesUnaccessedExpiredEntries(t *testing.T) {
	cache := NewTimerCache(1)
	cache.Set(1, &model.TimerDefinition{ID: 1}, -time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go cache.StartCleanup(ctx, time.Millisecond)

	deadline := time.Now().Add(100 * time.Millisecond)
	for cache.Size() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := cache.Size(); got != 0 {
		t.Fatalf("cache size = %d, want 0 after background cleanup", got)
	}
}
