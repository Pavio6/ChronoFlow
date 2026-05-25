package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

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
// 核心流程：
//  1. Bloom Filter 快速过滤已成功执行任务；命中后由 MySQL 确认
//  2. 查询完整的定时器定义（先内存缓存，miss 再 MySQL）
//  3. 使用定义中的状态判断是否仍处于 ACTIVE；缓存状态允许在 TTL 内滞后
//  4. 原子抢占执行记录：只有 PENDING -> RUNNING 成功的执行器继续
//  5. 执行 HTTP 回调
//  6. 执行成功后写 Bloom Filter，并更新 MySQL 最终状态
type Executor struct {
	defRepo    repository.TimerDefinitionRepository
	recRepo    repository.TimerRecordRepository
	bloom      *bloom.Filter
	cache      *memory.TimerCache
	reporter   *metrics.Reporter
	httpClient *http.Client
	cacheTTL   time.Duration
}

// NewExecutor 创建执行器实例
func NewExecutor(
	defRepo repository.TimerDefinitionRepository,
	recRepo repository.TimerRecordRepository,
	bloom *bloom.Filter,
	cache *memory.TimerCache,
	reporter *metrics.Reporter,
	cacheTTL time.Duration,
) *Executor {
	return &Executor{
		defRepo:    defRepo,
		recRepo:    recRepo,
		bloom:      bloom,
		cache:      cache,
		reporter:   reporter,
		httpClient: &http.Client{},
		cacheTTL:   cacheTTL,
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

	bloomKey := fmt.Sprintf("%s%s", redis.BloomPrefix, triggerTime.Format("2006-01-02"))
	bloomVal := fmt.Sprintf("%d:%d", timerID, triggerTime.UnixMilli())

	// 参考 xTimer：Bloom Filter 命中时再由 MySQL 确认，避免误判造成漏执行。
	mightExist, err := e.bloom.Exist(ctx, bloomKey, bloomVal)
	if err != nil {
		logger.Warn("Executor Bloom Filter 查询失败，继续使用数据库抢占",
			zap.Int64("timer_id", timerID),
			zap.Error(err),
		)
	} else if mightExist {
		started, err := e.recRepo.HasStartedByTimerIDAndTriggerTime(timerID, triggerTime)
		if err != nil {
			logger.Warn("Executor Bloom 命中后的 MySQL 确认失败，继续使用数据库抢占",
				zap.Int64("timer_id", timerID),
				zap.Error(err),
			)
		} else if started {
			logger.Debug("Executor Bloom 命中且任务已处理，跳过",
				zap.Int64("timer_id", timerID),
				zap.Time("trigger_time", triggerTime),
			)
			return
		}
	}

	// 与 xTimer 一致，从包含状态的本地定义缓存读取；状态变更允许在缓存 TTL 内滞后。
	def := e.getTimerDefinition(ctx, timerID)
	if def == nil {
		logger.Warn("Executor 定时器定义不存在",
			zap.Int64("timer_id", timerID),
		)
		return
	}
	if def.Status != model.TimerStatusActive {
		logger.Debug("Executor 定时器非激活状态，跳过",
			zap.Int64("timer_id", timerID),
		)
		return
	}

	// 原子抢占执行权；重复派发或并发节点只能有一个成功执行回调。
	now := time.Now()
	record, claimed, err := e.recRepo.ClaimPendingByTimerIDAndTriggerTime(timerID, triggerTime, now)
	if err != nil {
		logger.Error("Executor 抢占执行记录失败",
			zap.Int64("timer_id", timerID),
			zap.Error(err),
		)
		return
	}
	if !claimed {
		logger.Debug("Executor 任务已被领取或完成，跳过",
			zap.Int64("timer_id", timerID),
			zap.Time("trigger_time", triggerTime),
		)
		return
	}
	e.reporter.ReportTrigger()

	// 执行 HTTP 回调
	callbackStart := time.Now()
	responseCode, responseBody, err := e.executeHTTPCallback(def, record)
	callbackDuration := time.Since(callbackStart)

	// 计算执行耗时
	execDuration := time.Since(start)
	record.Duration = execDuration.Milliseconds()
	finishedAt := time.Now()
	record.FinishedAt = &finishedAt

	// 根据执行结果更新记录状态
	if err != nil {
		record.Status = model.RecordStatusFailed
		record.ErrorMessage = err.Error()

		e.reporter.ReportCallback(metrics.ResultFailed, callbackDuration)

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

		e.reporter.ReportCallback(metrics.ResultSuccess, callbackDuration)

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

}

// getTimerDefinition 获取包含状态的本地定义缓存；ACTIVE 状态可在 cacheTTL 内滞后。
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

	// 仅缓存激活定义。停用后旧缓存会在二级时间步内失效；重新激活时 miss 可直接回源。
	if def.Status == model.TimerStatusActive {
		e.cache.Set(timerID, def, e.cacheTTL)
	}

	return def
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
		var headers map[string]string
		if err := json.Unmarshal([]byte(def.CallbackHeaders), &headers); err != nil {
			return 0, "", fmt.Errorf("解析回调请求头失败: %w", err)
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}
	req.Header.Set("X-ChronoFlow-Timer-ID", fmt.Sprintf("%d", def.ID))

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
