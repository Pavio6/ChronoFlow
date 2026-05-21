package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/pkg/retry"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/pkg/logger"
	"go.uber.org/zap"
)

// Executor 任务执行器
// 负责执行 HTTP 回调，处理重试逻辑和超时控制
type Executor struct {
	taskRepo      repository.TaskRepository
	execRepo      repository.ExecutionRepository
	retryCalc     *retry.Calculator
	httpClient    *http.Client
	config        *config.ExecutorConfig
	retryConfig   *config.RetryConfig
	workerPool    chan struct{}
	metrics       *metrics.Reporter
}

// NewExecutor 创建执行器实例
func NewExecutor(
	taskRepo repository.TaskRepository,
	execRepo repository.ExecutionRepository,
	config *config.ExecutorConfig,
	retryConfig *config.RetryConfig,
) *Executor {
	// 创建重试计算器
	retryCalc := retry.NewCalculator(retry.Config{
		Strategy:        retry.Strategy(retryConfig.Strategy),
		InitialInterval: retryConfig.InitialInterval,
		MaxInterval:     retryConfig.MaxInterval,
		Multiplier:      retryConfig.Multiplier,
	})

	// 创建 HTTP 客户端
	httpClient := &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
	}

	return &Executor{
		taskRepo:    taskRepo,
		execRepo:    execRepo,
		retryCalc:   retryCalc,
		httpClient:  httpClient,
		config:      config,
		retryConfig: retryConfig,
		workerPool:  make(chan struct{}, config.WorkerPoolSize),
		metrics:     metrics.NewReporter(),
	}
}

// Submit 提交执行任务到工作池
func (e *Executor) Submit(execution *model.TaskExecution, task *model.Task) {
	go func() {
		// 获取工作池令牌（控制并发）
		e.workerPool <- struct{}{}
		defer func() { <-e.workerPool }()

		e.execute(context.Background(), execution, task)
	}()
}

// execute 执行任务
func (e *Executor) execute(ctx context.Context, execution *model.TaskExecution, task *model.Task) {
	// 更新执行状态为运行中
	now := time.Now()
	execution.Status = model.ExecutionStatusRUNNING
	execution.StartedAt = &now
	if err := e.execRepo.Update(execution); err != nil {
		logger.Error("failed to update execution status",
			zap.Int64("execution_id", execution.ID),
			zap.Error(err),
		)
		return
	}

	// 更新任务状态为运行中
	if err := e.taskRepo.UpdateStatus(task.ID, model.TaskStatusRUNNING); err != nil {
		logger.Error("failed to update task status",
			zap.Int64("task_id", task.ID),
			zap.Error(err),
		)
	}

	// 执行 HTTP 回调
	response, err := e.doHTTPRequest(ctx, execution)

	// 计算执行时长
	finishedAt := time.Now()
	execution.FinishedAt = &finishedAt
	execution.Duration = finishedAt.Sub(now).Milliseconds()

	// 上报监控指标
	taskIDStr := fmt.Sprintf("%d", task.ID)
	durationSeconds := float64(execution.Duration) / 1000.0
	e.metrics.ReportExecDuration(taskIDStr, durationSeconds)
	e.metrics.ReportTrigger(taskIDStr)

	if err != nil {
		// 执行失败
		execution.Status = model.ExecutionStatusFAILED
		execution.ErrorMessage = err.Error()

		// 上报失败监控
		e.metrics.ReportExecFailed(taskIDStr)
		e.metrics.ReportExecRecord(taskIDStr, "failed")

		logger.Error("task execution failed",
			zap.Int64("task_id", task.ID),
			zap.Int64("execution_id", execution.ID),
			zap.Error(err),
		)

		// 处理重试逻辑
		e.handleRetry(execution, task)
	} else {
		// 执行成功
		execution.Status = model.ExecutionStatusSUCCESS
		execution.ResponseCode = response.StatusCode
		execution.ResponseBody = response.Body

		// 上报成功监控
		e.metrics.ReportExecSuccess(taskIDStr)
		e.metrics.ReportExecRecord(taskIDStr, "success")

		// 更新任务状态为成功
		if err := e.taskRepo.UpdateStatus(task.ID, model.TaskStatusSUCCESS); err != nil {
			logger.Error("failed to update task status",
				zap.Int64("task_id", task.ID),
				zap.Error(err),
			)
		}

		logger.Info("task execution succeeded",
			zap.Int64("task_id", task.ID),
			zap.Int64("execution_id", execution.ID),
			zap.Int("status_code", response.StatusCode),
			zap.Int64("duration_ms", execution.Duration),
		)
	}

	// 保存执行记录
	if err := e.execRepo.Update(execution); err != nil {
		logger.Error("failed to update execution",
			zap.Int64("execution_id", execution.ID),
			zap.Error(err),
		)
	}
}

// HTTPResponse HTTP 响应封装
type HTTPResponse struct {
	StatusCode int
	Body       string
}

// doHTTPRequest 执行 HTTP 请求
func (e *Executor) doHTTPRequest(ctx context.Context, execution *model.TaskExecution) (*HTTPResponse, error) {
	// 解析回调头
	var headers map[string]string
	if execution.RequestBody != "" {
		// 尝试从任务配置中获取 headers
		// 这里简化处理，实际应该从任务配置中读取
	}

	// 创建请求体
	var bodyReader io.Reader
	if execution.RequestBody != "" {
		bodyReader = strings.NewReader(execution.RequestBody)
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, execution.RequestMethod, execution.RequestURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ChronoFlow/1.0")
	if headers != nil {
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}

	// 执行请求
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// 检查状态码
	if resp.StatusCode >= 400 {
		return &HTTPResponse{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}, fmt.Errorf("HTTP request returned error status: %d", resp.StatusCode)
	}

	return &HTTPResponse{
		StatusCode: resp.StatusCode,
		Body:       string(respBody),
	}, nil
}

// handleRetry 处理重试逻辑
func (e *Executor) handleRetry(execution *model.TaskExecution, task *model.Task) {
	taskIDStr := fmt.Sprintf("%d", task.ID)

	// 检查是否可重试
	if !execution.IsRetryable(task.MaxRetries) {
		// 超过最大重试次数，标记为失败
		execution.Status = model.ExecutionStatusFAILED
		e.taskRepo.UpdateStatus(task.ID, model.TaskStatusFAILED)

		logger.Warn("task exceeded max retries",
			zap.Int64("task_id", task.ID),
			zap.Int64("execution_id", execution.ID),
			zap.Int("retry_count", execution.RetryCount),
			zap.Int("max_retries", task.MaxRetries),
		)
		return
	}

	// 计算下次重试时间
	nextRetryTime := e.retryCalc.CalculateNextRetryTime(execution.RetryCount)
	execution.NextRetryTime = &nextRetryTime
	execution.RetryCount++
	execution.Status = model.ExecutionStatusRETRYING

	// 上报重试监控
	e.metrics.ReportExecRetry(taskIDStr)
	e.metrics.ReportExecRecord(taskIDStr, "retry")

	logger.Info("task scheduled for retry",
		zap.Int64("task_id", task.ID),
		zap.Int64("execution_id", execution.ID),
		zap.Int("retry_count", execution.RetryCount),
		zap.Time("next_retry_time", nextRetryTime),
	)
}

// ProcessRetries 处理待重试的任务
// 由定时任务调用，检查并重新执行待重试的任务
func (e *Executor) ProcessRetries(ctx context.Context) {
	executions, err := e.execRepo.GetPendingRetries()
	if err != nil {
		logger.Error("failed to get pending retries", zap.Error(err))
		return
	}

	for _, execution := range executions {
		// 获取任务详情
		task, err := e.taskRepo.GetByID(execution.TaskID)
		if err != nil {
			logger.Error("failed to get task for retry",
				zap.Int64("execution_id", execution.ID),
				zap.Error(err),
			)
			continue
		}

		// 重新提交执行
		e.Submit(execution, task)
	}
}

// HandleTimeouts 处理超时的执行
// 检查运行中超时的任务，标记为超时状态
func (e *Executor) HandleTimeouts(ctx context.Context) {
	timeout := time.Duration(e.config.Timeout) * time.Second
	executions, err := e.execRepo.GetRunningExecutions(timeout)
	if err != nil {
		logger.Error("failed to get running executions", zap.Error(err))
		return
	}

	for _, execution := range executions {
		execution.Status = model.ExecutionStatusTIMEOUT
		execution.ErrorMessage = "execution timeout"
		finishedAt := time.Now()
		execution.FinishedAt = &finishedAt

		if err := e.execRepo.Update(execution); err != nil {
			logger.Error("failed to update timeout execution",
				zap.Int64("execution_id", execution.ID),
				zap.Error(err),
			)
			continue
		}

		// 处理重试
		task, err := e.taskRepo.GetByID(execution.TaskID)
		if err != nil {
			continue
		}
		e.handleRetry(execution, task)

		logger.Warn("task execution timeout",
			zap.Int64("task_id", execution.TaskID),
			zap.Int64("execution_id", execution.ID),
		)
	}
}
