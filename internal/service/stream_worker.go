package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/callback"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/pkg/pool"
	redisstream "github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/pkg/logger"
	"go.uber.org/zap"
)

type workerStream interface {
	ReadNew(
		context.Context,
		string,
		string,
		string,
		int64,
		time.Duration,
	) ([]redisstream.StreamMessage, error)
	AutoClaim(
		context.Context,
		string,
		string,
		string,
		time.Duration,
		string,
		int64,
	) ([]redisstream.StreamMessage, string, error)
	Ack(context.Context, string, string, string) error
	PendingCount(context.Context, string, string) (int64, error)
}

type executionCallback interface {
	Execute(
		context.Context,
		*model.CallbackSnapshot,
		int64,
	) (int, string, error)
}

// StreamWorker consumes at-least-once Stream messages and uses the MySQL
// execution Lease as the authoritative processing claim.
type StreamWorker struct {
	repo       repository.TimerExecutionRepository
	stream     workerStream
	pool       pool.WorkerPool
	callback   executionCallback
	reporter   *metrics.Reporter
	workerCfg  config.WorkerConfig
	outboxCfg  config.OutboxConfig
	consumerID string
	now        func() time.Time
}

func NewStreamWorker(
	repo repository.TimerExecutionRepository,
	stream workerStream,
	workerPool pool.WorkerPool,
	callbackClient executionCallback,
	reporter *metrics.Reporter,
	workerCfg *config.WorkerConfig,
	outboxCfg *config.OutboxConfig,
	consumerID string,
) *StreamWorker {
	return &StreamWorker{
		repo:       repo,
		stream:     stream,
		pool:       workerPool,
		callback:   callbackClient,
		reporter:   reporter,
		workerCfg:  *workerCfg,
		outboxCfg:  *outboxCfg,
		consumerID: consumerID,
		now:        time.Now,
	}
}

func NewConfiguredCallbackClient(
	workerCfg *config.WorkerConfig,
	securityCfg *config.SecurityConfig,
) *callback.Client {
	return callback.NewClient(
		time.Duration(workerCfg.HTTPTimeoutSeconds)*time.Second,
		workerCfg.MaxResponseBytes,
		securityCfg.AllowPrivateCallbacks,
	)
}

func (w *StreamWorker) Start(ctx context.Context) {
	logger.Info("Redis Streams Worker 启动",
		zap.String("consumer", w.consumerID),
		zap.Int("pool_size", w.workerCfg.PoolSize),
	)
	reclaimCursor := "0-0"
	nextReclaim := w.now()
	for {
		if ctx.Err() != nil {
			logger.Info("Redis Streams Worker 停止", zap.String("consumer", w.consumerID))
			return
		}

		if !w.now().Before(nextReclaim) {
			messages, next, err := w.stream.AutoClaim(
				ctx,
				w.outboxCfg.Stream,
				w.outboxCfg.ConsumerGroup,
				w.consumerID,
				time.Duration(w.workerCfg.ReclaimIdleSeconds)*time.Second,
				reclaimCursor,
				w.workerCfg.ReadCount,
			)
			if err != nil {
				if ctx.Err() == nil {
					logger.Error("Worker 接管 Pending 消息失败", zap.Error(err))
				}
			} else {
				reclaimCursor = next
				if reclaimCursor == "" {
					reclaimCursor = "0-0"
				}
				for _, message := range messages {
					w.submit(ctx, message, true)
				}
			}
			nextReclaim = w.now().Add(
				time.Duration(w.workerCfg.ReclaimIntervalSeconds) * time.Second,
			)
		}

		messages, err := w.stream.ReadNew(
			ctx,
			w.outboxCfg.Stream,
			w.outboxCfg.ConsumerGroup,
			w.consumerID,
			w.workerCfg.ReadCount,
			time.Duration(w.workerCfg.ReadBlockMS)*time.Millisecond,
		)
		if err != nil {
			if ctx.Err() == nil {
				logger.Error("Worker 读取 Stream 失败", zap.Error(err))
			}
			continue
		}
		for _, message := range messages {
			w.submit(ctx, message, false)
		}
		w.refreshPendingMetric(ctx)
	}
}

func (w *StreamWorker) submit(
	ctx context.Context,
	message redisstream.StreamMessage,
	reclaimed bool,
) {
	if err := w.pool.Submit(func() {
		w.process(ctx, message, reclaimed)
	}); err != nil {
		logger.Error("Worker 提交 ants 任务失败",
			zap.String("message_id", message.ID),
			zap.Error(err),
		)
	}
}

func (w *StreamWorker) process(
	ctx context.Context,
	message redisstream.StreamMessage,
	reclaimed bool,
) {
	if message.DecodeError != "" || message.ExecutionID < 1 {
		logger.Error("Worker 丢弃无法解码的 Stream 消息",
			zap.String("message_id", message.ID),
			zap.String("error", message.DecodeError),
		)
		w.ack(ctx, message)
		return
	}
	if reclaimed {
		w.reporter.ReportWorkerRedelivery()
	}

	runToken, err := newRunToken()
	if err != nil {
		logger.Error("Worker 生成 run_token 失败", zap.Error(err))
		return
	}
	startedAt := w.now().UTC()
	execution, claimed, err := w.repo.Claim(
		ctx,
		message.ExecutionID,
		w.consumerID,
		runToken,
		startedAt,
		time.Duration(w.workerCfg.LeaseTTLSeconds)*time.Second,
	)
	if err != nil {
		logger.Error("Worker 抢占 Execution 失败",
			zap.Int64("execution_id", message.ExecutionID),
			zap.Error(err),
		)
		return
	}
	if execution == nil {
		logger.Warn("Worker 收到不存在的 Execution",
			zap.Int64("execution_id", message.ExecutionID),
		)
		w.ack(ctx, message)
		return
	}
	if !claimed {
		if execution.IsTerminal() || execution.Status == model.ExecutionStatusRetryWait {
			w.ack(ctx, message)
		}
		return
	}

	var snapshot model.CallbackSnapshot
	if err := json.Unmarshal([]byte(execution.RequestSnapshot), &snapshot); err != nil {
		w.finishFailure(
			message,
			execution,
			runToken,
			startedAt,
			0,
			"",
			fmt.Errorf("解析回调快照失败: %w", err),
			false,
		)
		return
	}

	callbackCtx, cancelCallback := context.WithCancel(ctx)
	var leaseLost atomic.Bool
	heartbeatDone := make(chan struct{})
	var heartbeatWG sync.WaitGroup
	heartbeatWG.Add(1)
	go func() {
		defer heartbeatWG.Done()
		w.heartbeat(
			callbackCtx,
			execution.ID,
			runToken,
			&leaseLost,
			cancelCallback,
			heartbeatDone,
		)
	}()

	responseCode, responseBody, callbackErr := w.callback.Execute(
		callbackCtx,
		&snapshot,
		execution.ID,
	)
	close(heartbeatDone)
	heartbeatWG.Wait()
	cancelCallback()
	if leaseLost.Load() {
		logger.Warn("Worker Lease 已丢失，拒绝提交过期执行结果",
			zap.Int64("execution_id", execution.ID),
		)
		w.reporter.ReportWorkerLeaseLost()
		return
	}

	if callbackErr != nil {
		w.finishFailure(
			message,
			execution,
			runToken,
			startedAt,
			responseCode,
			responseBody,
			callbackErr,
			isRetryableCallbackFailure(responseCode),
		)
		return
	}

	finishedAt := w.now().UTC()
	finalizeCtx, cancelFinalize := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFinalize()
	updated, err := w.repo.CompleteSuccess(
		finalizeCtx,
		execution.ID,
		w.consumerID,
		runToken,
		finishedAt,
		responseCode,
		responseBody,
		finishedAt.Sub(startedAt).Milliseconds(),
	)
	if err != nil || !updated {
		logger.Error("Worker 提交成功结果失败",
			zap.Int64("execution_id", execution.ID),
			zap.Bool("updated", updated),
			zap.Error(err),
		)
		return
	}
	w.reporter.ReportWorkerExecution(metrics.ResultSuccess, finishedAt.Sub(startedAt))
	w.ack(finalizeCtx, message)
}

func (w *StreamWorker) heartbeat(
	ctx context.Context,
	executionID int64,
	runToken string,
	leaseLost *atomic.Bool,
	cancel context.CancelFunc,
	done <-chan struct{},
) {
	ticker := time.NewTicker(
		time.Duration(w.workerCfg.HeartbeatSeconds) * time.Second,
	)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			leaseUntil := w.now().UTC().Add(
				time.Duration(w.workerCfg.LeaseTTLSeconds) * time.Second,
			)
			renewed, err := w.repo.Heartbeat(
				ctx,
				executionID,
				w.consumerID,
				runToken,
				leaseUntil,
			)
			if err != nil || !renewed {
				leaseLost.Store(true)
				cancel()
				logger.Error("Worker Execution Lease 续租失败",
					zap.Int64("execution_id", executionID),
					zap.Bool("renewed", renewed),
					zap.Error(err),
				)
				return
			}
		}
	}
}

func (w *StreamWorker) finishFailure(
	message redisstream.StreamMessage,
	execution *model.TimerExecution,
	runToken string,
	startedAt time.Time,
	responseCode int,
	responseBody string,
	callbackErr error,
	retryable bool,
) {
	finishedAt := w.now().UTC()
	retryAt := finishedAt.Add(w.retryBackoff(execution.Attempt))
	finalizeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updated, retryScheduled, err := w.repo.CompleteFailure(
		finalizeCtx,
		execution,
		w.consumerID,
		runToken,
		finishedAt,
		responseCode,
		responseBody,
		truncateWorkerError(callbackErr.Error()),
		finishedAt.Sub(startedAt).Milliseconds(),
		retryAt,
		retryable,
	)
	if err != nil || !updated {
		logger.Error("Worker 提交失败结果失败",
			zap.Int64("execution_id", execution.ID),
			zap.Bool("updated", updated),
			zap.Error(err),
		)
		return
	}
	w.reporter.ReportWorkerExecution(metrics.ResultFailed, finishedAt.Sub(startedAt))
	if retryScheduled {
		w.reporter.ReportWorkerRetry()
	}
	w.ack(finalizeCtx, message)
}

func (w *StreamWorker) ack(ctx context.Context, message redisstream.StreamMessage) {
	if err := w.stream.Ack(
		ctx,
		w.outboxCfg.Stream,
		w.outboxCfg.ConsumerGroup,
		message.ID,
	); err != nil {
		logger.Error("Worker XACK 失败",
			zap.String("message_id", message.ID),
			zap.Int64("execution_id", message.ExecutionID),
			zap.Error(err),
		)
	}
}

func (w *StreamWorker) refreshPendingMetric(ctx context.Context) {
	count, err := w.stream.PendingCount(
		ctx,
		w.outboxCfg.Stream,
		w.outboxCfg.ConsumerGroup,
	)
	if err == nil {
		w.reporter.SetWorkerPending(count)
	}
}

func (w *StreamWorker) retryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	exponent := attempt - 1
	if exponent > 30 {
		exponent = 30
	}
	seconds := int64(w.workerCfg.RetryBaseSeconds) * (int64(1) << exponent)
	if seconds > int64(w.workerCfg.RetryMaxSeconds) {
		seconds = int64(w.workerCfg.RetryMaxSeconds)
	}
	return time.Duration(seconds) * time.Second
}

func newRunToken() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func truncateWorkerError(message string) string {
	const maxLength = 2048
	message = strings.TrimSpace(message)
	if len(message) <= maxLength {
		return message
	}
	return message[:maxLength-3] + "..."
}

func isRetryableCallbackFailure(responseCode int) bool {
	if responseCode == 0 {
		return true
	}
	return responseCode == 408 ||
		responseCode == 425 ||
		responseCode == 429 ||
		responseCode >= 500
}
