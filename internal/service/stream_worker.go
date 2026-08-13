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
	"github.com/chronoflow/internal/pkg/logger"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/pkg/pool"
	redisstream "github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/internal/repository"
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

// StreamWorker 消费至少一次投递的 Stream 消息，并以 MySQL Execution Lease 作为权威领取依据
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

// NewStreamWorker 创建消费 Redis Stream 并执行回调的 Worker
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

// NewConfiguredCallbackClient 根据 Worker 与安全配置创建回调 HTTP 客户端
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

// Start 持续读取新消息和超时 Pending 消息，并提交给协程池处理
func (w *StreamWorker) Start(ctx context.Context) {
	logger.Info("Redis Streams worker started",
		zap.String("consumer", w.consumerID),
		zap.Int("pool_size", w.workerCfg.PoolSize),
	)
	reclaimCursor := "0-0"
	nextReclaim := w.now()
	for {
		if ctx.Err() != nil {
			logger.Info("Redis Streams worker stopped", zap.String("consumer", w.consumerID))
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
					logger.Error("Worker failed to reclaim pending messages", zap.Error(err))
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
				logger.Error("Worker failed to read stream", zap.Error(err))
			}
			continue
		}
		for _, message := range messages {
			w.submit(ctx, message, false)
		}
		w.refreshPendingMetric(ctx)
	}
}

// submit 将一条 Stream 消息提交给协程池异步处理
func (w *StreamWorker) submit(
	ctx context.Context,
	message redisstream.StreamMessage,
	reclaimed bool,
) {
	if err := w.pool.Submit(func() {
		w.process(ctx, message, reclaimed)
	}); err != nil {
		logger.Error("Worker failed to submit task to ants pool",
			zap.String("message_id", message.ID),
			zap.Error(err),
		)
	}
}

// process 领取 Execution、执行回调并按最终结果确认或安排重试
func (w *StreamWorker) process(
	ctx context.Context,
	message redisstream.StreamMessage,
	reclaimed bool,
) {
	if message.DecodeError != "" || message.ExecutionID < 1 {
		logger.Error("Worker discarded malformed stream message",
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
		logger.Error("Worker failed to generate run token", zap.Error(err))
		return
	}
	startedAt := w.now()
	execution, claimed, err := w.repo.Claim(
		ctx,
		message.ExecutionID,
		w.consumerID,
		runToken,
		startedAt,
		time.Duration(w.workerCfg.LeaseTTLSeconds)*time.Second,
	)
	if err != nil {
		logger.Error("Worker failed to claim execution",
			zap.Int64("execution_id", message.ExecutionID),
			zap.Error(err),
		)
		return
	}
	if execution == nil {
		logger.Warn("Worker received a missing execution",
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
			fmt.Errorf("decode callback snapshot: %w", err),
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
		logger.Warn("Worker lease lost; refusing to persist stale execution result",
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

	finishedAt := w.now()
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
		logger.Error("Worker failed to persist successful execution result",
			zap.Int64("execution_id", execution.ID),
			zap.Bool("updated", updated),
			zap.Error(err),
		)
		return
	}
	w.reporter.ReportWorkerExecution(metrics.ResultSuccess, finishedAt.Sub(startedAt))
	w.ack(finalizeCtx, message)
}

// heartbeat 在回调进行期间定期续约当前 Execution 的 Lease
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
			leaseUntil := w.now().Add(
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
				logger.Error("Worker failed to renew execution lease",
					zap.Int64("execution_id", executionID),
					zap.Bool("renewed", renewed),
					zap.Error(err),
				)
				return
			}
		}
	}
}

// finishFailure 记录回调失败，并在可重试时创建后续投递事件
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
	finishedAt := w.now()
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
		logger.Error("Worker failed to persist failed execution result",
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

// ack 确认一条已完成处理的 Stream 消息
func (w *StreamWorker) ack(ctx context.Context, message redisstream.StreamMessage) {
	if err := w.stream.Ack(
		ctx,
		w.outboxCfg.Stream,
		w.outboxCfg.ConsumerGroup,
		message.ID,
	); err != nil {
		logger.Error("Worker failed to acknowledge stream message",
			zap.String("message_id", message.ID),
			zap.Int64("execution_id", message.ExecutionID),
			zap.Error(err),
		)
	}
}

// refreshPendingMetric 刷新当前 Consumer Group 的 Pending 消息指标
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

// retryBackoff 根据尝试次数计算受上限限制的重试退避时间
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

// newRunToken 生成用于拒绝过期执行结果的随机令牌
func newRunToken() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

// truncateWorkerError 清理并截断准备持久化的 Worker 错误信息
func truncateWorkerError(message string) string {
	const maxLength = 2048
	message = strings.TrimSpace(message)
	if len(message) <= maxLength {
		return message
	}
	return message[:maxLength-3] + "..."
}

// isRetryableCallbackFailure 判断回调状态码是否允许安排重试
func isRetryableCallbackFailure(responseCode int) bool {
	if responseCode == 0 {
		return true
	}
	return responseCode == 408 ||
		responseCode == 425 ||
		responseCode == 429 ||
		responseCode >= 500
}
