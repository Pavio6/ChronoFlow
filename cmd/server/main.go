package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/handler"
	"github.com/chronoflow/internal/middleware"
	"github.com/chronoflow/internal/pkg/bloom"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/internal/service"
	"github.com/chronoflow/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	// 加载配置
	cfg, err := config.Load("./config")
	if err != nil {
		fmt.Printf("failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志
	logger.Init(
		cfg.Log.Level,
		cfg.Log.Format,
		cfg.Log.Output,
		cfg.Log.FilePath,
	)
	defer logger.Sync()

	logger.Info("starting ChronoFlow server",
		zap.Int("port", cfg.Server.Port),
		zap.String("mode", cfg.Server.Mode),
	)

	// 初始化数据库
	if err := repository.InitDatabase(&cfg.Database); err != nil {
		logger.Fatal("failed to init database", zap.Error(err))
	}
	defer repository.CloseDatabase()

	// 自动迁移数据库表
	if err := repository.AutoMigrate(); err != nil {
		logger.Fatal("failed to auto migrate", zap.Error(err))
	}

	// 初始化 Redis
	redisClient, err := redis.InitRedis(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		logger.Fatal("failed to init redis", zap.Error(err))
	}
	defer redisClient.Close()

	// 创建 Redis 队列
	redisQueue := redis.NewRedisQueue(redisClient)

	// 创建布隆过滤器
	bloomFilter := bloom.NewFilter(redisClient)

	// 创建仓库层
	taskRepo := repository.NewTaskRepository(repository.DB)
	execRepo := repository.NewExecutionRepository(repository.DB)

	// 创建服务层
	taskService := service.NewTaskService(taskRepo, execRepo)
	execService := service.NewExecutionService(execRepo)

	// 创建执行器
	executor := service.NewExecutor(taskRepo, execRepo, &cfg.Executor, &cfg.Retry)

	// 创建调度器
	scheduler := service.NewScheduler(taskRepo, redisQueue, &cfg.Scheduler)

	// 创建触发器
	trigger := service.NewTrigger(taskRepo, execRepo, redisQueue, executor, bloomFilter, &cfg.Scheduler)

	// 创建 Prometheus 监控
	metricsReporter := metrics.NewReporter()
	_ = metricsReporter

	// 创建 Gin 引擎
	gin.SetMode(cfg.Server.Mode)
	r := gin.New()

	// 注册中间件
	r.Use(middleware.Recovery())
	r.Use(middleware.Logger())
	r.Use(middleware.CORS())

	// 注册路由
	taskHandler := handler.NewTaskHandler(taskService, execService)
	taskHandler.RegisterRoutes(r)

	// 静态文件服务 - 前端资源
	frontendDir := "./web/dist"
	if _, err := os.Stat(frontendDir); err == nil {
		// 服务静态资源文件
		r.Static("/assets", filepath.Join(frontendDir, "assets"))

		// 处理 SPA 路由 - 所有非 API 请求返回 index.html
		r.NoRoute(func(c *gin.Context) {
			// 如果是 API 请求，返回 404
			if strings.HasPrefix(c.Request.URL.Path, "/api") {
				c.JSON(http.StatusNotFound, gin.H{
					"code":    404,
					"message": "API not found",
				})
				return
			}

			// 否则返回 index.html（支持前端路由）
			c.File(filepath.Join(frontendDir, "index.html"))
		})

		logger.Info("frontend static files served", zap.String("dir", frontendDir))
	} else {
		logger.Warn("frontend directory not found, skipping static file serving")
	}

	// 创建 HTTP 服务器
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: r,
	}

	// 创建上下文用于优雅关闭
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动调度器
	go scheduler.Start(ctx)

	// 启动触发器
	go trigger.Start(ctx)

	// 启动重试处理器
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				executor.ProcessRetries(ctx)
			}
		}
	}()

	// 启动超时处理器
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				executor.HandleTimeouts(ctx)
			}
		}
	}()

	// 启动 HTTP 服务器
	go func() {
		logger.Info("HTTP server started", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("failed to start server", zap.Error(err))
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...")

	// 取消上下文，停止调度器和触发器
	cancel()

	// 停止调度器和触发器
	scheduler.Stop()
	trigger.Stop()

	// 优雅关闭 HTTP 服务器
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server forced to shutdown", zap.Error(err))
	}

	logger.Info("server exited")
}
