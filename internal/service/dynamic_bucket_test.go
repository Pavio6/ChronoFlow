package service

import (
	"testing"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/pkg/redis"
)

func TestMergeTaskTriggersDeduplicatesRedisAndDatabaseResults(t *testing.T) {
	at := time.Now().Truncate(time.Second)
	redisTasks := []*redis.TaskTrigger{
		{TimerID: 1, TriggerTime: at},
		{TimerID: 2, TriggerTime: at.Add(time.Second)},
	}
	dbTasks := []*redis.TaskTrigger{
		{TimerID: 1, TriggerTime: at},
		{TimerID: 3, TriggerTime: at.Add(2 * time.Second)},
	}

	got := mergeTaskTriggers(redisTasks, dbTasks)
	if len(got) != 3 {
		t.Fatalf("merged trigger count = %d, want 3", len(got))
	}
	if got[0].TimerID != 1 || got[1].TimerID != 2 || got[2].TimerID != 3 {
		t.Fatalf("merged triggers = %+v, want redis order followed by non-duplicate DB tasks", got)
	}
}

func TestDynamicBucketExpirationCoversFutureSliceAndRetention(t *testing.T) {
	cfg := &config.SchedulerConfig{BucketMetadataTTL: 600}
	futureSlice := time.Now().Add(2 * time.Minute).Truncate(time.Minute)

	expiration, err := dynamicBucketExpiration(formatTimeRange(futureSlice), cfg)
	if err != nil {
		t.Fatalf("dynamicBucketExpiration returned error: %v", err)
	}
	if expiration <= 10*time.Minute {
		t.Fatalf("future slice expiration = %v, want longer than configured retention", expiration)
	}
}

func TestDynamicBucketExpirationUsesRetentionForPastSlice(t *testing.T) {
	cfg := &config.SchedulerConfig{BucketMetadataTTL: 600}
	pastSlice := time.Now().Add(-time.Hour).Truncate(time.Minute)

	expiration, err := dynamicBucketExpiration(formatTimeRange(pastSlice), cfg)
	if err != nil {
		t.Fatalf("dynamicBucketExpiration returned error: %v", err)
	}
	if expiration != 10*time.Minute {
		t.Fatalf("past slice expiration = %v, want 10m", expiration)
	}
}
