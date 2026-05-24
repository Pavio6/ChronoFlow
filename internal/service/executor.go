package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/bloom"
	"github.com/chronoflow/internal/pkg/memory"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/pkg/logger"
	"go.uber.org/zap"
)

// Executor 执行器
// 被 Trigger 启动后，执行单个定时任务
// 核心流程（两层幂等去重）：
//  1. Bloom Filter 快速查重
//  2. 若命中，查 MySQL 记录状态确认
//  3. 查询定时器定义（先内存缓存，miss 再 MySQL）
//  4. 执行 HTTP 回调
//  5. 执行成功 → Bloom Filter 打点
//  6. 更新 MySQL 执行记录状态
type Executor struct {
	defRepo    repository.TimerDefinitionRepository
	recRepo    repository.TimerRecordRepository
	bloom      *bloom.Filter
	cache      *memory.TimerCache
	reporter   *metrics.Reporter
	httpClient *http.Client
	cfg        *config.ExecutorConfig
}

// NewExecutor 创建执行器实例
func NewExecutor(
	defRepo repository.TimerDefinitionRepository,
	recRepo repository.TimerRecordRepository,
	bloom *bloom.Filter,
	cache *memory.TimerCache,
	reporter *metrics.Reporter,
	cfg *config.ExecutorConfig,
) *Executor {
	// 创建带超时的 HTTP 客户端
	httpClient := &http.Client{
		Timeout: time.Duration(cfg.Timeout) * time.Second,
	}

	return &Executor{
		defRepo:    defRepo,
		recRepo:    recRepo,
		bloom:      bloom,
		cache:      cache,
		reporter:   reporter,
		httpClient: httpClient,
		cfg:        cfg,
	}
}

// Execute 执行单个定时任务
// trigger: 包含 TimerID 和 TriggerTime 的触发信息
// 此方法由 Trigger 通过协程池调用
func (e *Executor) Execute(ctx context.Context, trigger *redis.TaskTrigger) {
	start := time.Now()
	timerID := trigger.TimerID
	triggerTime := trigger.TriggerTime

	logger.Debug("Executor 开始执行",
		zap.Int64("timer_id", timerID),
		zap.Time("trigger_time", triggerTime),
	)

	// 第一层：Bloom Filter 快速查重
	bloomKey := fmt.Sprintf("%s%s", redis.BloomPrefix, time.Now().Format("2006-01-02"))
	bloomVal := fmt.Sprintf("%d:%d", timerID, triggerTime.UnixMilli())
	mightExist, err := e.bloom.Exist(ctx, bloomKey, bloomVal)
	if err != nil {
		logger.Error("Executor Bloom Filter 查询失败",
			zap.Int64("timer_id", timerID),
			zap.Error(err),
		)
		// Bloom Filter 故障时继续执行（宁可重复执行，不可漏执行）
	}

	if mightExist {
		// 第二层：MySQL 查重确认
		exists, err := e.recRepo.ExistsByTimerIDAndTriggerTime(timerID, triggerTime)
		if err != nil {
			logger.Error("Executor MySQL 幂等检查失败",
				zap.Int64("timer_id", timerID),
				zap.Error(err),
			)
		}

		if exists {
			logger.Debug("Executor 任务已执行，跳过",
				zap.Int64("timer_id", timerID),
				zap.Time("trigger_time", triggerTime),
			)
			return
		}
	}

	// 查询定时器定义（先内存缓存，miss 再 MySQL）
	def := e.getTimerDefinition(ctx, timerID)
	if def == nil {
		logger.Warn("Executor 定时器定义不存在",
			zap.Int64("timer_id", timerID),
		)
		return
	}

	// 检查定时器状态，INACTIVE 或 DELETED 则跳过
	if def.Status != model.TimerStatusActive {
		logger.Debug("Executor 定时器非激活状态，跳过",
			zap.Int64("timer_id", timerID),
			zap.String("status", string(def.Status)),
		)
		return
	}
	e.reporter.ReportTrigger(def.App)

	// 查找对应的执行记录
	record, err := e.findOrCreateRecord(ctx, timerID, triggerTime, def)
	if err != nil {
		logger.Error("Executor 查找/创建执行记录失败",
			zap.Int64("timer_id", timerID),
			zap.Error(err),
		)
		return
	}

	// 更新记录状态为 RUNNING
	now := time.Now()
	record.Status = model.RecordStatusRunning
	record.StartedAt = &now
	if err := e.recRepo.Update(record); err != nil {
		logger.Error("Executor 更新记录状态为 RUNNING 失败",
			zap.Int64("record_id", record.ID),
			zap.Error(err),
		)
	}

	// 执行 HTTP 回调
	responseCode, responseBody, err := e.executeHTTPCallback(def, record)

	// 计算执行耗时
	execDuration := time.Since(start)
	record.Duration = execDuration.Milliseconds()
	finishedAt := time.Now()
	record.FinishedAt = &finishedAt

	// 根据执行结果更新记录状态
	if err != nil {
		// 执行失败
		record.Status = model.RecordStatusFailed
		record.ErrorMessage = err.Error()

		// 上报失败指标
		e.reporter.ReportExecFailed(timerID, def.App, "error")

		logger.Error("Executor 任务执行失败",
			zap.Int64("timer_id", timerID),
			zap.Error(err),
			zap.Duration("duration", execDuration),
		)
	} else {
		// 执行成功
		record.Status = model.RecordStatusSuccess
		record.ResponseCode = responseCode
		record.ResponseBody = responseBody

		// Bloom Filter 打点（仅成功时写入，避免失败任务被误判为已完成）
		if err := e.bloom.Set(ctx, bloomKey, bloomVal, 86400); err != nil {
			logger.Error("Executor Bloom Filter 设置失败",
				zap.Int64("timer_id", timerID),
				zap.Error(err),
			)
		}

		// 上报成功指标
		e.reporter.ReportExecSuccess(timerID, def.App)

		logger.Info("Executor 任务执行成功",
			zap.Int64("timer_id", timerID),
			zap.Int("response_code", responseCode),
			zap.Duration("duration", execDuration),
		)
	}

	// 更新执行记录
	if err := e.recRepo.Update(record); err != nil {
		logger.Error("Executor 更新执行记录失败",
			zap.Int64("record_id", record.ID),
			zap.Error(err),
		)
	}

	// 上报执行指标
	e.reporter.ReportExecRecord(timerID, def.App)
	e.reporter.ReportExecDuration(timerID, def.App, float64(execDuration.Milliseconds()))
}

// getTimerDefinition 获取定时器定义（先内存缓存，miss 再 MySQL）
func (e *Executor) getTimerDefinition(ctx context.Context, timerID int64) *model.TimerDefinition {
	// 先查内存缓存
	if def, ok := e.cache.Get(timerID); ok {
		return def
	}

	// 缓存未命中，查 MySQL
	def, err := e.defRepo.GetByID(timerID)
	if err != nil {
		logger.Error("Executor 查询定时器定义失败",
			zap.Int64("timer_id", timerID),
			zap.Error(err),
		)
		return nil
	}

	if def == nil {
		return nil
	}

	// 写入缓存（TTL 为 step2_duration，即二级迁移间隔）
	e.cache.Set(timerID, def, 5*time.Minute)

	return def
}

// findOrCreateRecord 查找或创建执行记录
func (e *Executor) findOrCreateRecord(ctx context.Context, timerID int64, triggerTime time.Time, def *model.TimerDefinition) (*model.TimerRecord, error) {
	// 先尝试查找已存在的 PENDING 记录
	records, err := e.recRepo.GetByTimerID(timerID, 10)
	if err != nil {
		return nil, fmt.Errorf("查询执行记录失败: %w", err)
	}

	// 查找匹配的 PENDING 记录
	for _, r := range records {
		if r.TriggerTime.Equal(triggerTime) && r.Status == model.RecordStatusPending {
			return r, nil
		}
	}

	// 未找到，创建新记录
	record := &model.TimerRecord{
		TimerID:       timerID,
		TriggerTime:   triggerTime,
		Status:        model.RecordStatusPending,
		RequestURL:    def.CallbackURL,
		RequestMethod: def.CallbackMethod,
		RequestBody:   def.CallbackBody,
	}

	if err := e.recRepo.Create(record); err != nil {
		return nil, fmt.Errorf("创建执行记录失败: %w", err)
	}

	return record, nil
}

// executeHTTPCallback 执行 HTTP 回调
// 返回：响应状态码、响应体、错误
func (e *Executor) executeHTTPCallback(def *model.TimerDefinition, record *model.TimerRecord) (int, string, error) {
	// 构建请求体
	var bodyReader io.Reader
	if record.RequestBody != "" {
		bodyReader = bytes.NewBufferString(record.RequestBody)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequest(record.RequestMethod, record.RequestURL, bodyReader)
	if err != nil {
		return 0, "", fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	// 设置默认请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ChronoFlow/1.0")

	// 设置自定义请求头
	if def.CallbackHeaders != "" {
		// 简单解析 JSON 格式的 headers
		// 这里简化处理，实际应该用 json.Unmarshal
		req.Header.Set("X-ChronoFlow-Timer-ID", fmt.Sprintf("%d", def.ID))
	}

	// 执行请求
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("执行 HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应体（限制最大 1MB）
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return resp.StatusCode, "", fmt.Errorf("读取响应体失败: %w", err)
	}

	// 非 2xx 状态码视为失败
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, string(respBody), fmt.Errorf("HTTP 回调返回非 2xx 状态码: %d", resp.StatusCode)
	}

	return resp.StatusCode, string(respBody), nil
}
