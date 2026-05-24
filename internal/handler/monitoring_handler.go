package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/pkg/metrics"
	redisqueue "github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/internal/repository"
	"github.com/gin-gonic/gin"
)

// MonitoringHandler 提供前端监控页使用的聚合数据
type MonitoringHandler struct {
	defRepo  repository.TimerDefinitionRepository
	recRepo  repository.TimerRecordRepository
	queue    *redisqueue.RedisQueue
	reporter *metrics.Reporter
}

// NewMonitoringHandler 创建监控处理器
func NewMonitoringHandler(
	defRepo repository.TimerDefinitionRepository,
	recRepo repository.TimerRecordRepository,
	queue *redisqueue.RedisQueue,
	reporter *metrics.Reporter,
) *MonitoringHandler {
	return &MonitoringHandler{
		defRepo:  defRepo,
		recRepo:  recRepo,
		queue:    queue,
		reporter: reporter,
	}
}

// RegisterRoutes 注册监控 API
func (h *MonitoringHandler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/api/v1")
	api.GET("/monitoring/summary", h.GetSummary)
}

// GetSummary 返回运行时监控摘要
func (h *MonitoringHandler) GetSummary(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	timerStats, err := h.defRepo.CountByStatus()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
		return
	}

	recordStats, err := h.recRepo.CountByStatus()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
		return
	}

	queueStats, err := h.queue.Stats(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"data": gin.H{
			"timers": gin.H{
				"total":    sumTimerStats(timerStats),
				"active":   timerStats[model.TimerStatusActive],
				"inactive": timerStats[model.TimerStatusInactive],
				"deleted":  timerStats[model.TimerStatusDeleted],
			},
			"records": gin.H{
				"total":   sumRecordStats(recordStats),
				"pending": recordStats[model.RecordStatusPending],
				"running": recordStats[model.RecordStatusRunning],
				"success": recordStats[model.RecordStatusSuccess],
				"failed":  recordStats[model.RecordStatusFailed],
				"timeout": recordStats[model.RecordStatusTimeout],
			},
			"redis":    queueStats,
			"runtime":  h.reporter.Snapshot(),
			"exporter": "/metrics",
		},
	})
}

func sumTimerStats(stats map[model.TimerStatus]int64) int64 {
	var total int64
	for _, count := range stats {
		total += count
	}
	return total
}

func sumRecordStats(stats map[model.RecordStatus]int64) int64 {
	var total int64
	for _, count := range stats {
		total += count
	}
	return total
}
