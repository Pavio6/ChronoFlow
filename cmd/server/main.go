package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/handler"
	"github.com/chronoflow/internal/middleware"
	"github.com/chronoflow/internal/pkg/bloom"
	"github.com/chronoflow/internal/pkg/cron"
	"github.com/chronoflow/internal/pkg/memory"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/pkg/pool"
	redisqueue "github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/internal/service"
	"github.com/chronoflow/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	// ========== 1. 加载配置 ==========
	cfg, err := config.Load("./config")
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}

	// ========== 2. 初始化日志 ==========
	logger.Init(cfg.Log.Level, cfg.Log.Format, cfg.Log.Output, cfg.Log.FilePath)
	defer logger.Sync()
	logger.Info("ChronoFlow 启动中...")

	// ========== 3. 初始化数据库 ==========
	if err := repository.InitDatabase(&cfg.Database); err != nil {
		logger.Fatal("初始化数据库失败", zap.Error(err))
	}
	defer repository.CloseDatabase()
	logger.Info("数据库连接成功")

	// 自动迁移表结构
	if err := repository.AutoMigrate(); err != nil {
		logger.Fatal("数据库表迁移失败", zap.Error(err))
	}
	logger.Info("数据库表迁移完成")

	// ========== 4. 初始化 Redis ==========
	redisClient, err := redisqueue.InitRedis(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		logger.Fatal("初始化 Redis 失败", zap.Error(err))
	}
	defer redisClient.Close()
	logger.Info("Redis 连接成功")

	// ========== 5. 初始化基础设施组件 ==========
	// Redis 任务队列
	queue := redisqueue.NewRedisQueue(redisClient)

	// Bloom Filter
	bloomFilter := bloom.NewFilter(redisClient)

	// 内存缓存（最大 10000 条）
	timerCache := memory.NewTimerCache(10000)
	timerCacheTTL := time.Duration(cfg.Scheduler.Step2Duration) * time.Second

	// Cron 表达式解析器
	cronParser := cron.NewCronParser()

	// Prometheus 指标上报器
	reporter := metrics.NewReporter()

	// ========== 6. 初始化仓库层 ==========
	defRepo := repository.NewTimerDefinitionRepository(repository.DB)
	recRepo := repository.NewTimerRecordRepository(repository.DB)

	// ========== 7. 初始化协程池 ==========
	schedulerPool, err := pool.NewGoWorkerPool(cfg.Scheduler.WorkerPoolSize)
	if err != nil {
		logger.Fatal("创建调度协程池失败", zap.Error(err))
	}
	defer schedulerPool.Release()

	triggerPool, err := pool.NewGoWorkerPool(cfg.Trigger.WorkerPoolSize)
	if err != nil {
		logger.Fatal("创建执行协程池失败", zap.Error(err))
	}
	defer triggerPool.Release()
	logger.Info("协程池创建成功",
		zap.Int("scheduler_pool_size", cfg.Scheduler.WorkerPoolSize),
		zap.Int("trigger_pool_size", cfg.Trigger.WorkerPoolSize),
	)

	// ========== 8. 初始化服务层 ==========
	// 执行器（Bloom 快速过滤，MySQL 条件状态更新授予唯一执行权）
	executor := service.NewExecutor(
		defRepo, recRepo, bloomFilter, timerCache,
		reporter, timerCacheTTL,
	)

	// 触发器（被 Scheduler 调用，DB 部分投递补偿需要 recRepo）
	trigger := service.NewTrigger(queue, triggerPool, executor, recRepo)

	// 调度器
	scheduler := service.NewScheduler(queue, schedulerPool, trigger, recRepo, &cfg.Scheduler)

	// 迁移器
	migrator := service.NewMigrator(defRepo, recRepo, queue, cronParser, &cfg.Scheduler)

	// 定时器 CRUD 服务
	timerService := service.NewTimerService(defRepo, recRepo, cronParser, queue, &cfg.Scheduler)

	// 当前状态指标采集器
	monitorCollector := service.NewMonitorCollector(recRepo, queue, reporter, &cfg.Monitoring)

	// ========== 9. 启动后台服务 ==========
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 清理不再访问的过期本地定义缓存。
	go timerCache.StartCleanup(ctx, timerCacheTTL)

	// 启动迁移器（一级迁移：MySQL -> Redis）
	go migrator.Start(ctx)

	// 启动调度器（轮询 + 分发）
	go scheduler.Start(ctx)

	// 定期采集记录积压与 Redis 队列状态
	go monitorCollector.Start(ctx)

	logger.Info("后台服务已启动",
		zap.Int("migrate_step_minutes", cfg.Scheduler.MigrateStepMinutes),
		zap.Int("step2_duration", cfg.Scheduler.Step2Duration),
		zap.Int("bucket_num", cfg.Scheduler.BucketNum),
		zap.Int("monitor_collect_interval_seconds", cfg.Monitoring.CollectIntervalSeconds),
	)

	// ========== 10. 配置 HTTP 服务器 ==========
	gin.SetMode(cfg.Server.Mode)
	r := gin.New()

	// 注册中间件
	r.Use(middleware.Logger())
	r.Use(middleware.CORS())

	// 注册 API 路由
	timerHandler := handler.NewTimerHandler(timerService)
	timerHandler.RegisterRoutes(r)
	monitoringHandler := handler.NewMonitoringHandler(defRepo, recRepo, queue, reporter, &cfg.Monitoring)
	monitoringHandler.RegisterRoutes(r)

	// Prometheus 指标端点
	r.GET("/metrics", gin.WrapH(reporter.GetHandler()))

	// 健康检查端点
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	// Grafana dashboard is embedded in the management page through this same-origin path.
	if cfg.Monitoring.GrafanaURL != "" {
		grafanaTarget, err := url.Parse(cfg.Monitoring.GrafanaURL)
		if err != nil {
			logger.Fatal("Grafana 代理地址无效", zap.Error(err))
		}
		grafanaProxy := httputil.NewSingleHostReverseProxy(grafanaTarget)
		r.Any("/grafana", gin.WrapH(grafanaProxy))
		r.Any("/grafana/*path", gin.WrapH(grafanaProxy))
	}

	// 静态文件服务（前端）
	r.Static("/assets", "./web/dist/assets")
	r.StaticFile("/", "./web/dist/index.html")
	r.NoRoute(func(c *gin.Context) {
		if c.Request.URL.Path == "/api" || strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    http.StatusNotFound,
				"message": "接口不存在",
			})
			return
		}
		// SPA 路由回退：非 API 请求返回 index.html。
		c.File("./web/dist/index.html")
	})

	// ========== 11. 启动 HTTP 服务器 ==========
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		logger.Info("HTTP 服务器启动", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("HTTP 服务器启动失败", zap.Error(err))
		}
	}()

	// ========== 12. 优雅关闭 ==========
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("收到关闭信号，开始优雅关闭...", zap.String("signal", sig.String()))

	// 停止后台服务
	migrator.Stop()
	scheduler.Stop()
	cancel()

	// 关闭 HTTP 服务器（等待 5 秒处理完当前请求）
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP 服务器关闭失败", zap.Error(err))
	}

	logger.Info("ChronoFlow 已关闭")
}
