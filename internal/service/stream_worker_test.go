package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/logger"
	"github.com/chronoflow/internal/pkg/metrics"
	redisstream "github.com/chronoflow/internal/pkg/redis"
)

type immediateWorkerPool struct{}

// Submit 为测试替身或测试辅助代码提供所需行为
func (immediateWorkerPool) Submit(task func()) error {
	task()
	return nil
}

type stubWorkerStream struct {
	acked []string
}

// ReadNew 为测试替身或测试辅助代码提供所需行为
func (s *stubWorkerStream) ReadNew(
	context.Context, string, string, string, int64, time.Duration,
) ([]redisstream.StreamMessage, error) {
	return nil, nil
}

// AutoClaim 为测试替身或测试辅助代码提供所需行为
func (s *stubWorkerStream) AutoClaim(
	context.Context, string, string, string, time.Duration, string, int64,
) ([]redisstream.StreamMessage, string, error) {
	return nil, "0-0", nil
}

// Ack 为测试替身或测试辅助代码提供所需行为
func (s *stubWorkerStream) Ack(
	_ context.Context,
	_ string,
	_ string,
	messageID string,
) error {
	s.acked = append(s.acked, messageID)
	return nil
}

// PendingCount 为测试替身或测试辅助代码提供所需行为
func (s *stubWorkerStream) PendingCount(
	context.Context, string, string,
) (int64, error) {
	return 0, nil
}

type stubExecutionRepo struct {
	execution        *model.TimerExecution
	claimed          bool
	successUpdated   bool
	failureUpdated   bool
	retryScheduled   bool
	successCalls     int
	failureCalls     int
	failureRetryable bool
	heartbeatResult  bool
}

// Claim 为测试替身或测试辅助代码提供所需行为
func (r *stubExecutionRepo) Claim(
	context.Context, int64, string, string, time.Time, time.Duration,
) (*model.TimerExecution, bool, error) {
	return r.execution, r.claimed, nil
}

// Heartbeat 为测试替身或测试辅助代码提供所需行为
func (r *stubExecutionRepo) Heartbeat(
	context.Context, int64, string, string, time.Time,
) (bool, error) {
	return r.heartbeatResult, nil
}

// CompleteSuccess 为测试替身或测试辅助代码提供所需行为
func (r *stubExecutionRepo) CompleteSuccess(
	context.Context, int64, string, string, time.Time, int, string, int64,
) (bool, error) {
	r.successCalls++
	return r.successUpdated, nil
}

// CompleteFailure 为测试替身或测试辅助代码提供所需行为
func (r *stubExecutionRepo) CompleteFailure(
	_ context.Context,
	_ *model.TimerExecution,
	_ string,
	_ string,
	_ time.Time,
	_ int,
	_ string,
	_ string,
	_ int64,
	_ time.Time,
	retryable bool,
) (bool, bool, error) {
	r.failureCalls++
	r.failureRetryable = retryable
	return r.failureUpdated, r.retryScheduled && retryable, nil
}

// CountByStatus 为测试替身或测试辅助代码提供所需行为
func (r *stubExecutionRepo) CountByStatus(
	context.Context,
) (map[model.ExecutionStatus]int64, error) {
	return nil, nil
}

type stubCallback struct {
	code int
	body string
	err  error
}

// Execute 为测试替身或测试辅助代码提供所需行为
func (c *stubCallback) Execute(
	context.Context,
	*model.CallbackSnapshot,
	int64,
) (int, string, error) {
	return c.code, c.body, c.err
}

// newTestStreamWorker 为测试替身或测试辅助代码提供所需行为
func newTestStreamWorker(
	repo *stubExecutionRepo,
	stream *stubWorkerStream,
	callbackClient *stubCallback,
) *StreamWorker {
	initOutboxTestLogger.Do(func() {
		logger.Init("error", "console", "stdout", "")
	})
	worker := NewStreamWorker(
		repo,
		stream,
		immediateWorkerPool{},
		callbackClient,
		metrics.NewReporter(),
		&config.WorkerConfig{
			PoolSize:           1,
			LeaseTTLSeconds:    30,
			HeartbeatSeconds:   10,
			RetryBaseSeconds:   2,
			RetryMaxSeconds:    30,
			MaxResponseBytes:   1024,
			HTTPTimeoutSeconds: 1,
		},
		&config.OutboxConfig{
			Stream:        "stream",
			ConsumerGroup: "group",
		},
		"worker-test",
	)
	worker.now = func() time.Time {
		return time.Date(2026, time.August, 7, 10, 0, 0, 0, time.Local)
	}
	return worker
}

// testClaimedExecution 为测试替身或测试辅助代码提供所需行为
func testClaimedExecution() *model.TimerExecution {
	return &model.TimerExecution{
		ID:              10,
		Status:          model.ExecutionStatusRunning,
		Attempt:         1,
		MaxAttempts:     3,
		RequestSnapshot: `{"url":"https://example.com","method":"POST"}`,
	}
}

// TestStreamWorkerCompletesAndAcknowledgesSuccess 验证对应的测试场景
func TestStreamWorkerCompletesAndAcknowledgesSuccess(t *testing.T) {
	repo := &stubExecutionRepo{
		execution:      testClaimedExecution(),
		claimed:        true,
		successUpdated: true,
	}
	stream := &stubWorkerStream{}
	worker := newTestStreamWorker(repo, stream, &stubCallback{code: 204})

	worker.process(context.Background(), redisstream.StreamMessage{
		ID:          "1-0",
		ExecutionID: 10,
	}, false)

	if repo.successCalls != 1 || repo.failureCalls != 0 {
		t.Fatalf("success calls=%d failure calls=%d", repo.successCalls, repo.failureCalls)
	}
	if len(stream.acked) != 1 || stream.acked[0] != "1-0" {
		t.Fatalf("acked = %v, want [1-0]", stream.acked)
	}
}

// TestStreamWorkerSchedulesRetryBeforeAck 验证对应的测试场景
func TestStreamWorkerSchedulesRetryBeforeAck(t *testing.T) {
	repo := &stubExecutionRepo{
		execution:      testClaimedExecution(),
		claimed:        true,
		failureUpdated: true,
		retryScheduled: true,
	}
	stream := &stubWorkerStream{}
	worker := newTestStreamWorker(
		repo,
		stream,
		&stubCallback{err: errors.New("temporary failure")},
	)

	worker.process(context.Background(), redisstream.StreamMessage{
		ID:          "2-0",
		ExecutionID: 10,
	}, false)

	if repo.failureCalls != 1 || repo.successCalls != 0 {
		t.Fatalf("failure calls=%d success calls=%d", repo.failureCalls, repo.successCalls)
	}
	if !repo.failureRetryable {
		t.Fatal("network failure must be retryable")
	}
	if len(stream.acked) != 1 || stream.acked[0] != "2-0" {
		t.Fatalf("acked = %v, want [2-0]", stream.acked)
	}
}

// TestStreamWorkerDoesNotRetryPermanentHTTPFailure 验证对应的测试场景
func TestStreamWorkerDoesNotRetryPermanentHTTPFailure(t *testing.T) {
	repo := &stubExecutionRepo{
		execution:      testClaimedExecution(),
		claimed:        true,
		failureUpdated: true,
		retryScheduled: true,
	}
	stream := &stubWorkerStream{}
	worker := newTestStreamWorker(
		repo,
		stream,
		&stubCallback{
			code: http.StatusBadRequest,
			err:  errors.New("invalid request"),
		},
	)

	worker.process(context.Background(), redisstream.StreamMessage{
		ID:          "2-permanent",
		ExecutionID: 10,
	}, false)

	if repo.failureRetryable {
		t.Fatal("HTTP 400 must be a permanent failure")
	}
	if len(stream.acked) != 1 {
		t.Fatalf("acked = %v, want terminal failure ACK", stream.acked)
	}
}

// TestStreamWorkerAcknowledgesTerminalDuplicate 验证对应的测试场景
func TestStreamWorkerAcknowledgesTerminalDuplicate(t *testing.T) {
	repo := &stubExecutionRepo{
		execution: &model.TimerExecution{
			ID:     10,
			Status: model.ExecutionStatusSuccess,
		},
	}
	stream := &stubWorkerStream{}
	worker := newTestStreamWorker(repo, stream, &stubCallback{})

	worker.process(context.Background(), redisstream.StreamMessage{
		ID:          "3-0",
		ExecutionID: 10,
	}, true)

	if len(stream.acked) != 1 {
		t.Fatalf("acked = %v, want one terminal duplicate ack", stream.acked)
	}
}

// TestStreamWorkerDoesNotAckWhenFinalStateIsRejected 验证对应的测试场景
func TestStreamWorkerDoesNotAckWhenFinalStateIsRejected(t *testing.T) {
	repo := &stubExecutionRepo{
		execution: testClaimedExecution(),
		claimed:   true,
	}
	stream := &stubWorkerStream{}
	worker := newTestStreamWorker(repo, stream, &stubCallback{code: 200})

	worker.process(context.Background(), redisstream.StreamMessage{
		ID:          "4-0",
		ExecutionID: 10,
	}, false)

	if len(stream.acked) != 0 {
		t.Fatalf("acked = %v, want none after stale run_token rejection", stream.acked)
	}
}
