package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chronoflow/internal/config"
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
	promURL  string
	client   *http.Client
}

// NewMonitoringHandler 创建监控处理器
func NewMonitoringHandler(
	defRepo repository.TimerDefinitionRepository,
	recRepo repository.TimerRecordRepository,
	queue *redisqueue.RedisQueue,
	reporter *metrics.Reporter,
	cfg *config.MonitoringConfig,
) *MonitoringHandler {
	return &MonitoringHandler{
		defRepo:  defRepo,
		recRepo:  recRepo,
		queue:    queue,
		reporter: reporter,
		promURL:  cfg.PrometheusURL,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

// RegisterRoutes 注册监控 API
func (h *MonitoringHandler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/api/v1")
	api.GET("/monitoring/summary", h.GetSummary)
	api.GET("/monitoring/history", h.GetHistory)
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
			},
			"redis":    queueStats,
			"runtime":  h.reporter.Snapshot(),
			"exporter": "/metrics",
		},
	})
}

type historyPoint struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

type prometheusRangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Values [][]json.RawMessage `json:"values"`
		} `json:"result"`
	} `json:"data"`
	Error string `json:"error"`
}

var historyQueries = map[string]string{
	"availability":     `up{job="chronoflow"}`,
	"success_rate":     `100 * sum(rate(chronoflow_callback_requests_total{result="success"}[5m])) / clamp_min(sum(rate(chronoflow_callback_requests_total[5m])), 1e-9)`,
	"callback_p95_ms":  `1000 * histogram_quantile(0.95, sum by (le) (rate(chronoflow_callback_duration_seconds_bucket[5m])))`,
	"abnormal_records": `chronoflow_pending_overdue_records + chronoflow_running_stale_records`,
}

// GetHistory returns a fixed set of historical series stored by Prometheus.
func (h *MonitoringHandler) GetHistory(c *gin.Context) {
	rangeMinutes, stepSeconds := historyWindow(c.Query("range_minutes"))
	end := time.Now()
	start := end.Add(-time.Duration(rangeMinutes) * time.Minute)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 6*time.Second)
	defer cancel()

	series := make(map[string][]historyPoint, len(historyQueries))
	var (
		queryErr error
		mu       sync.Mutex
		wg       sync.WaitGroup
	)
	for name, query := range historyQueries {
		name, query := name, query
		wg.Add(1)
		go func() {
			defer wg.Done()
			points, err := h.queryRange(ctx, query, start, end, stepSeconds)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && queryErr == nil {
				queryErr = err
				return
			}
			series[name] = points
		}()
	}
	wg.Wait()
	if queryErr != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": http.StatusBadGateway, "message": queryErr.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"data": gin.H{
			"range_minutes": rangeMinutes,
			"step_seconds":  stepSeconds,
			"source":        "prometheus",
			"series":        series,
		},
	})
}

func historyWindow(raw string) (int, int) {
	switch raw {
	case "15":
		return 15, 10
	case "360":
		return 360, 120
	case "1440":
		return 1440, 300
	default:
		return 60, 30
	}
}

func (h *MonitoringHandler) queryRange(ctx context.Context, query string, start, end time.Time, stepSeconds int) ([]historyPoint, error) {
	endpoint, err := url.Parse(strings.TrimRight(h.promURL, "/") + "/api/v1/query_range")
	if err != nil {
		return nil, fmt.Errorf("Prometheus 地址无效: %w", err)
	}
	params := endpoint.Query()
	params.Set("query", query)
	params.Set("start", strconv.FormatInt(start.Unix(), 10))
	params.Set("end", strconv.FormatInt(end.Unix(), 10))
	params.Set("step", strconv.Itoa(stepSeconds))
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("创建 Prometheus 查询失败: %w", err)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("查询 Prometheus 历史指标失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("查询 Prometheus 历史指标返回状态 %d", resp.StatusCode)
	}

	var result prometheusRangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 Prometheus 历史指标失败: %w", err)
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("Prometheus 查询失败: %s", result.Error)
	}
	if len(result.Data.Result) == 0 {
		return []historyPoint{}, nil
	}

	points := make([]historyPoint, 0, len(result.Data.Result[0].Values))
	for _, rawPoint := range result.Data.Result[0].Values {
		if len(rawPoint) != 2 {
			continue
		}
		var timestamp float64
		var valueText string
		if err := json.Unmarshal(rawPoint[0], &timestamp); err != nil {
			continue
		}
		if err := json.Unmarshal(rawPoint[1], &valueText); err != nil {
			continue
		}
		value, err := strconv.ParseFloat(valueText, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		points = append(points, historyPoint{Timestamp: int64(timestamp), Value: value})
	}
	return points, nil
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
