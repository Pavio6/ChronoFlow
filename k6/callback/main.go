package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const maxDiscardBodyBytes = 1 << 20

type callbackConfig struct {
	address string
	status  int
	delay   time.Duration
}

type callbackStats struct {
	Total                 int64     `json:"total"`
	Unique                int64     `json:"unique"`
	Duplicates            int64     `json:"duplicates"`
	MissingIdempotencyKey int64     `json:"missing_idempotency_key"`
	StartedAt             time.Time `json:"started_at"`
}

type collector struct {
	total      atomic.Int64
	unique     atomic.Int64
	duplicates atomic.Int64
	missing    atomic.Int64
	keys       sync.Map
	startedAt  atomic.Pointer[time.Time]
}

// newCollector 创建新的并发安全回调计数器
func newCollector() *collector {
	collector := &collector{}
	collector.reset()
	return collector
}

// record 记录一次回调及其幂等标识
func (c *collector) record(idempotencyKey string) {
	c.total.Add(1)
	if idempotencyKey == "" {
		c.missing.Add(1)
		return
	}
	if _, loaded := c.keys.LoadOrStore(idempotencyKey, struct{}{}); loaded {
		c.duplicates.Add(1)
		return
	}
	c.unique.Add(1)
}

// snapshot 返回当前计数快照
func (c *collector) snapshot() callbackStats {
	startedAt := c.startedAt.Load()
	return callbackStats{
		Total:                 c.total.Load(),
		Unique:                c.unique.Load(),
		Duplicates:            c.duplicates.Load(),
		MissingIdempotencyKey: c.missing.Load(),
		StartedAt:             *startedAt,
	}
}

// reset 清空全部回调统计
func (c *collector) reset() {
	c.total.Store(0)
	c.unique.Store(0)
	c.duplicates.Store(0)
	c.missing.Store(0)
	c.keys.Clear()
	now := time.Now()
	c.startedAt.Store(&now)
}

// newHandler 创建回调、统计、重置和健康检查接口
func newHandler(collector *collector, cfg callbackConfig) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, io.LimitReader(request.Body, maxDiscardBodyBytes))
		_ = request.Body.Close()
		collector.record(request.Header.Get("Idempotency-Key"))
		if cfg.delay > 0 {
			time.Sleep(cfg.delay)
		}
		writer.WriteHeader(cfg.status)
	})
	mux.HandleFunc("/stats", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(collector.snapshot()); err != nil {
			slog.Error("encode callback statistics", slog.Any("error", err))
		}
	})
	mux.HandleFunc("/reset", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		collector.reset()
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/health", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})
	return mux
}

// main 启动仅供本地压测使用的轻量回调接收器
func main() {
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("load callback receiver configuration", slog.Any("error", err))
		os.Exit(1)
	}
	server := &http.Server{
		Addr:              cfg.address,
		Handler:           newHandler(newCollector(), cfg),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
			slog.Error("shut down callback receiver", slog.Any("error", shutdownErr))
		}
	}()

	slog.Info("callback receiver started",
		slog.String("address", cfg.address),
		slog.Int("status", cfg.status),
		slog.Duration("delay", cfg.delay),
	)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("serve callback receiver", slog.Any("error", err))
		os.Exit(1)
	}
}

// loadConfig 从环境变量读取回调接收器配置
func loadConfig() (callbackConfig, error) {
	cfg := callbackConfig{
		address: envOrDefault("CALLBACK_ADDR", "127.0.0.1:9091"),
		status:  http.StatusNoContent,
	}
	status, err := parseIntegerEnv("CALLBACK_STATUS", cfg.status)
	if err != nil || status < 200 || status > 599 {
		return callbackConfig{}, fmt.Errorf("CALLBACK_STATUS must be an integer between 200 and 599")
	}
	delayMS, err := parseIntegerEnv("CALLBACK_DELAY_MS", 0)
	if err != nil || delayMS < 0 {
		return callbackConfig{}, fmt.Errorf("CALLBACK_DELAY_MS must be a non-negative integer")
	}
	cfg.status = status
	cfg.delay = time.Duration(delayMS) * time.Millisecond
	return cfg, nil
}

// parseIntegerEnv 读取整数环境变量
func parseIntegerEnv(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

// envOrDefault 读取字符串环境变量
func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
