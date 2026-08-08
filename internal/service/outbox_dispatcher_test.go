package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/logger"
	"github.com/chronoflow/internal/pkg/metrics"
)

var initOutboxTestLogger sync.Once

type stubOutboxRepo struct {
	events        []*model.OutboxEvent
	claimErr      error
	published     []string
	failed        []string
	nextAttemptAt time.Time
	unpublished   int64
}

func (r *stubOutboxRepo) ClaimBatch(
	context.Context,
	string,
	time.Time,
	int,
	time.Duration,
) ([]*model.OutboxEvent, error) {
	return r.events, r.claimErr
}

func (r *stubOutboxRepo) MarkPublished(
	_ context.Context,
	eventID string,
	_ string,
	_ string,
	_ time.Time,
) error {
	r.published = append(r.published, eventID)
	return nil
}

func (r *stubOutboxRepo) MarkFailed(
	_ context.Context,
	eventID string,
	_ string,
	_ string,
	nextAttemptAt time.Time,
) error {
	r.failed = append(r.failed, eventID)
	r.nextAttemptAt = nextAttemptAt
	return nil
}

func (r *stubOutboxRepo) CountUnpublished(context.Context) (int64, error) {
	return r.unpublished, nil
}

type stubStreamPublisher struct {
	messageID string
	err       error
	published []string
}

func (p *stubStreamPublisher) Publish(
	_ context.Context,
	_ string,
	_ int64,
	event *model.OutboxEvent,
) (string, error) {
	p.published = append(p.published, event.EventID)
	return p.messageID, p.err
}

func newTestOutboxDispatcher(
	repo *stubOutboxRepo,
	publisher *stubStreamPublisher,
) *OutboxDispatcher {
	initOutboxTestLogger.Do(func() {
		logger.Init("error", "console", "stdout", "")
	})
	dispatcher := NewOutboxDispatcher(
		repo,
		publisher,
		metrics.NewReporter(),
		&config.OutboxConfig{
			BatchSize:         100,
			ClaimTTLSeconds:   30,
			MaxBackoffSeconds: 30,
			Stream:            "test-stream",
			StreamMaxLen:      1000,
		},
		"dispatcher-test",
	)
	dispatcher.now = func() time.Time {
		return time.Date(2026, time.August, 7, 10, 0, 0, 0, time.Local)
	}
	return dispatcher
}

func TestOutboxDispatcherPublishesAndMarksEvent(t *testing.T) {
	repo := &stubOutboxRepo{
		events: []*model.OutboxEvent{{EventID: "evt-1", AggregateID: 10}},
	}
	publisher := &stubStreamPublisher{messageID: "1-0"}
	dispatcher := newTestOutboxDispatcher(repo, publisher)

	dispatcher.dispatchOnce(context.Background())

	if len(publisher.published) != 1 || publisher.published[0] != "evt-1" {
		t.Fatalf("published = %v, want [evt-1]", publisher.published)
	}
	if len(repo.published) != 1 || repo.published[0] != "evt-1" {
		t.Fatalf("marked published = %v, want [evt-1]", repo.published)
	}
	if len(repo.failed) != 0 {
		t.Fatalf("marked failed = %v, want none", repo.failed)
	}
}

func TestOutboxDispatcherRecordsFailureWithBackoff(t *testing.T) {
	repo := &stubOutboxRepo{
		events: []*model.OutboxEvent{{EventID: "evt-2", AggregateID: 20, Attempts: 2}},
	}
	publisher := &stubStreamPublisher{err: errors.New("redis unavailable")}
	dispatcher := newTestOutboxDispatcher(repo, publisher)

	dispatcher.dispatchOnce(context.Background())

	if len(repo.failed) != 1 || repo.failed[0] != "evt-2" {
		t.Fatalf("marked failed = %v, want [evt-2]", repo.failed)
	}
	wantNext := dispatcher.now().Add(4 * time.Second)
	if !repo.nextAttemptAt.Equal(wantNext) {
		t.Fatalf("next attempt = %s, want %s", repo.nextAttemptAt, wantNext)
	}
	if len(repo.published) != 0 {
		t.Fatalf("marked published = %v, want none", repo.published)
	}
}

func TestOutboxDispatcherBackoffIsCapped(t *testing.T) {
	dispatcher := newTestOutboxDispatcher(&stubOutboxRepo{}, &stubStreamPublisher{})
	if got := dispatcher.backoff(100); got != 30*time.Second {
		t.Fatalf("backoff = %s, want 30s", got)
	}
}
