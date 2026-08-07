package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/pkg/cron"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/pkg/pool"
	redisstream "github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/internal/service"
	"github.com/chronoflow/pkg/logger"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type backgroundService struct {
	name string
	run  func(context.Context)
}

// Application owns the process-local resources and lifecycle for one role.
type Application struct {
	cfg         *config.Config
	role        Role
	server      *http.Server
	sqlDB       *sql.DB
	redisClient *goredis.Client
	pools       []*pool.GoWorkerPool
	background  []backgroundService
	closeOnce   sync.Once
}

// New constructs the resources needed by a single process role.
func New(cfg *config.Config, role Role) (*Application, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if !role.IsValid() {
		return nil, fmt.Errorf("invalid role %q", role)
	}

	application := &Application{
		cfg:  cfg,
		role: role,
	}
	cleanupOnError := func(err error) (*Application, error) {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			time.Duration(cfg.Runtime.ShutdownTimeoutSeconds)*time.Second,
		)
		defer cancel()
		application.closeWithContext(cleanupCtx)
		return nil, err
	}

	if err := repository.InitDatabase(&cfg.Database); err != nil {
		return cleanupOnError(fmt.Errorf("初始化数据库失败: %w", err))
	}
	sqlDB, err := repository.DB.DB()
	if err != nil {
		return cleanupOnError(fmt.Errorf("获取数据库连接失败: %w", err))
	}
	application.sqlDB = sqlDB

	// AutoMigrate is an explicit local-development option. Production runs
	// migrations as a single release job before any role starts.
	if role.RunsAPI() && cfg.Database.AutoMigrate {
		if err := repository.AutoMigrate(); err != nil {
			return cleanupOnError(fmt.Errorf("数据库表迁移失败: %w", err))
		}
	}

	var (
		streamPublisher *redisstream.StreamPublisher
		streamConsumer  *redisstream.StreamConsumer
	)
	if roleRequiresRedis(role) {
		redisClient, err := redisstream.InitRedis(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
		if err != nil {
			return cleanupOnError(fmt.Errorf("初始化 Redis 失败: %w", err))
		}
		application.redisClient = redisClient
		streamPublisher = redisstream.NewStreamPublisher(redisClient)
		streamConsumer = redisstream.NewStreamConsumer(redisClient)
	}

	reporter := metrics.NewReporter()
	defRepo := repository.NewTimerDefinitionRepository(repository.DB)
	cronParser := cron.NewCronParser()
	dueTimerRepo := repository.NewDueTimerRepository(repository.DB)
	outboxRepo := repository.NewOutboxRepository(repository.DB)
	executionRepo := repository.NewTimerExecutionRepository(repository.DB)
	executionQueryRepo := repository.NewExecutionQueryRepository(repository.DB)
	recoveryRepo := repository.NewRecoveryRepository(repository.DB)

	var apiDeps *apiDependencies
	if role.RunsAPI() {
		apiDeps = &apiDependencies{
			timerService: service.NewTimerService(
				defRepo,
				cronParser,
				&cfg.Scheduler,
				&cfg.Security,
			),
			defRepo:       defRepo,
			executionRepo: executionQueryRepo,
		}
	}

	if role == RoleScheduler || role == RoleAll {
		application.buildScheduler(dueTimerRepo, cronParser, reporter)
		application.buildReconciler(recoveryRepo, executionRepo, reporter)
		if role == RoleAll {
			if err := application.buildOutboxDispatcher(
				outboxRepo,
				streamPublisher,
				reporter,
			); err != nil {
				return cleanupOnError(err)
			}
			if err := application.buildStreamWorker(
				executionRepo,
				streamPublisher,
				streamConsumer,
				reporter,
			); err != nil {
				return cleanupOnError(err)
			}
		}
	}
	if role == RoleDispatcher {
		if err := application.buildOutboxDispatcher(
			outboxRepo,
			streamPublisher,
			reporter,
		); err != nil {
			return cleanupOnError(err)
		}
	}
	if role == RoleWorker {
		if err := application.buildStreamWorker(
			executionRepo,
			streamPublisher,
			streamConsumer,
			reporter,
		); err != nil {
			return cleanupOnError(err)
		}
	}

	httpHandler, err := newHTTPHandler(
		cfg,
		role,
		reporter,
		application.checkReadiness,
		apiDeps,
	)
	if err != nil {
		return cleanupOnError(fmt.Errorf("初始化 HTTP 路由失败: %w", err))
	}
	application.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           httpHandler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return application, nil
}

func roleRequiresRedis(role Role) bool {
	return role == RoleDispatcher || role == RoleWorker || role == RoleAll
}

func (a *Application) buildScheduler(
	repo repository.DueTimerRepository,
	cronParser *cron.CronParser,
	reporter *metrics.Reporter,
) {
	scheduler := service.NewScheduler(repo, cronParser, reporter, &a.cfg.Scheduler)
	a.background = append(a.background, backgroundService{
		name: "scheduler",
		run:  scheduler.Start,
	})
}

func (a *Application) buildReconciler(
	recoveryRepo repository.RecoveryRepository,
	executionRepo repository.TimerExecutionRepository,
	reporter *metrics.Reporter,
) {
	reconciler := service.NewReconciler(
		recoveryRepo,
		executionRepo,
		reporter,
		&a.cfg.Recovery,
	)
	a.background = append(a.background, backgroundService{
		name: "execution-reconciler",
		run:  reconciler.Start,
	})
}

func (a *Application) buildStreamWorker(
	executionRepo repository.TimerExecutionRepository,
	streamPublisher *redisstream.StreamPublisher,
	streamConsumer *redisstream.StreamConsumer,
	reporter *metrics.Reporter,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := streamPublisher.EnsureConsumerGroup(
		ctx,
		a.cfg.Outbox.Stream,
		a.cfg.Outbox.ConsumerGroup,
	); err != nil {
		return err
	}
	workerPool, err := pool.NewGoWorkerPool(a.cfg.Worker.PoolSize)
	if err != nil {
		return fmt.Errorf("创建 Worker ants Pool 失败: %w", err)
	}
	a.pools = append(a.pools, workerPool)

	worker := service.NewStreamWorker(
		executionRepo,
		streamConsumer,
		workerPool,
		service.NewConfiguredCallbackClient(&a.cfg.Worker, &a.cfg.Security),
		reporter,
		&a.cfg.Worker,
		&a.cfg.Outbox,
		a.instanceID(),
	)
	cleaner := service.NewStreamRetentionCleaner(
		streamConsumer,
		reporter,
		&a.cfg.Outbox,
		&a.cfg.Recovery,
	)
	a.background = append(a.background,
		backgroundService{name: "stream-worker", run: worker.Start},
		backgroundService{name: "stream-retention-cleaner", run: cleaner.Start},
	)
	return nil
}

func (a *Application) instanceID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s-%s-%d", a.role, hostname, os.Getpid())
}

func (a *Application) buildOutboxDispatcher(
	repo repository.OutboxRepository,
	streamPublisher *redisstream.StreamPublisher,
	reporter *metrics.Reporter,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := streamPublisher.EnsureConsumerGroup(
		ctx,
		a.cfg.Outbox.Stream,
		a.cfg.Outbox.ConsumerGroup,
	); err != nil {
		return err
	}

	dispatcher := service.NewOutboxDispatcher(
		repo,
		streamPublisher,
		reporter,
		&a.cfg.Outbox,
		a.instanceID(),
	)
	a.background = append(a.background, backgroundService{
		name: "outbox-dispatcher",
		run:  dispatcher.Start,
	})
	return nil
}

// Run starts the role and blocks until the context is cancelled or the HTTP
// listener fails.
func (a *Application) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	logger.Info("ChronoFlow 角色启动",
		zap.String("role", string(a.role)),
		zap.String("runtime_mode", a.role.RuntimeMode()),
		zap.Strings("components", a.role.Components()),
		zap.String("http_addr", a.server.Addr),
	)
	var backgroundWG sync.WaitGroup
	for _, component := range a.background {
		component := component
		backgroundWG.Add(1)
		go func() {
			defer backgroundWG.Done()
			logger.Info("后台组件启动", zap.String("component", component.name))
			component.run(runCtx)
			logger.Info("后台组件停止", zap.String("component", component.name))
		}()
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("HTTP 服务器启动",
			zap.String("role", string(a.role)),
			zap.String("addr", a.server.Addr),
		)
		err := a.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	var runErr error
	select {
	case <-ctx.Done():
		logger.Info("收到关闭信号，开始优雅关闭",
			zap.String("role", string(a.role)),
		)
	case err := <-serverErr:
		if err != nil {
			runErr = fmt.Errorf("HTTP 服务器运行失败: %w", err)
		}
	}

	cancel()
	shutdownTimeout := time.Duration(a.cfg.Runtime.ShutdownTimeoutSeconds) * time.Second
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	if err := a.server.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = fmt.Errorf("HTTP 服务器关闭失败: %w", err)
	}

	backgroundDone := make(chan struct{})
	go func() {
		backgroundWG.Wait()
		close(backgroundDone)
	}()
	select {
	case <-backgroundDone:
	case <-shutdownCtx.Done():
		if runErr == nil {
			runErr = fmt.Errorf("等待后台组件关闭超时: %w", shutdownCtx.Err())
		}
	}

	a.closeWithContext(shutdownCtx)
	logger.Info("ChronoFlow 角色已停止", zap.String("role", string(a.role)))
	return runErr
}

func (a *Application) checkReadiness(ctx context.Context) map[string]string {
	failures := make(map[string]string)
	if err := a.sqlDB.PingContext(ctx); err != nil {
		failures["mysql"] = err.Error()
	}
	if a.redisClient != nil {
		if err := a.redisClient.Ping(ctx).Err(); err != nil {
			failures["redis"] = err.Error()
		}
	}
	return failures
}

// Close releases process-local pools and external connections.
func (a *Application) Close() {
	timeout := time.Duration(a.cfg.Runtime.ShutdownTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	a.closeWithContext(ctx)
}

func (a *Application) closeWithContext(ctx context.Context) {
	a.closeOnce.Do(func() {
		for _, workerPool := range a.pools {
			if err := workerPool.ReleaseContext(ctx); err != nil {
				logger.Warn("协程池未在超时时间内停止", zap.Error(err))
			}
		}
		if a.redisClient != nil {
			if err := a.redisClient.Close(); err != nil {
				logger.Warn("关闭 Redis 连接失败", zap.Error(err))
			}
		}
		repository.CloseDatabase()
	})
}
