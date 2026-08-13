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
	"github.com/chronoflow/internal/pkg/logger"
	"github.com/chronoflow/internal/repository"
	"go.uber.org/zap"
)

type backgroundService struct {
	name string
	run  func(context.Context)
}

// Application 管理一个可独立部署角色的生命周期和本地资源
type Application struct {
	cfg        *config.Config
	role       Role
	server     *http.Server
	sqlDB      *sql.DB
	background []backgroundService
	closers    []func(context.Context)
	closeOnce  sync.Once
}

// newApplication 初始化角色共享的数据库连接和应用实例
func newApplication(cfg *config.Config, role Role) (*Application, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

	application := &Application{cfg: cfg, role: role}
	if err := repository.InitDatabase(&cfg.Database); err != nil {
		return application.fail(fmt.Errorf("initialize database: %w", err))
	}
	sqlDB, err := repository.DB.DB()
	if err != nil {
		return application.fail(fmt.Errorf("get database connection: %w", err))
	}
	application.sqlDB = sqlDB
	return application, nil
}

// fail 关闭已初始化的资源并返回原始错误
func (a *Application) fail(err error) (*Application, error) {
	shutdownTimeout := time.Duration(a.cfg.Runtime.ShutdownTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	a.closeWithContext(ctx)
	return nil, err
}

// setHTTPServer 设置当前角色对外提供的 HTTP 服务
func (a *Application) setHTTPServer(handler http.Handler) {
	a.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", a.cfg.Server.Port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// addCloser 按注册顺序保存需要在关闭时执行的资源释放函数
func (a *Application) addCloser(closer func(context.Context)) {
	a.closers = append(a.closers, closer)
}

// instanceID 生成当前角色实例的进程唯一标识
func (a *Application) instanceID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s-%s-%d", a.role, hostname, os.Getpid())
}

// Run 启动当前角色并阻塞至收到停止信号或服务异常退出
func (a *Application) Run(ctx context.Context) error {
	if a.server == nil {
		return errors.New("HTTP server is not configured")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	logger.Info("ChronoFlow role started",
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
			logger.Info("Background component started", zap.String("component", component.name))
			component.run(runCtx)
			logger.Info("Background component stopped", zap.String("component", component.name))
		}()
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("HTTP server started",
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
		logger.Info("Shutdown signal received; starting graceful shutdown", zap.String("role", string(a.role)))
	case err := <-serverErr:
		if err != nil {
			runErr = fmt.Errorf("run HTTP server: %w", err)
		}
	}

	cancel()
	shutdownTimeout := time.Duration(a.cfg.Runtime.ShutdownTimeoutSeconds) * time.Second
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	if err := a.server.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = fmt.Errorf("shut down HTTP server: %w", err)
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
			runErr = fmt.Errorf("timed out waiting for background components to stop: %w", shutdownCtx.Err())
		}
	}

	a.closeWithContext(shutdownCtx)
	logger.Info("ChronoFlow role stopped", zap.String("role", string(a.role)))
	return runErr
}

// Close 在默认超时时间内释放当前角色的进程本地资源
func (a *Application) Close() {
	timeout := time.Duration(a.cfg.Runtime.ShutdownTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	a.closeWithContext(ctx)
}

// closeWithContext 按相反注册顺序关闭资源及数据库连接
func (a *Application) closeWithContext(ctx context.Context) {
	a.closeOnce.Do(func() {
		for index := len(a.closers) - 1; index >= 0; index-- {
			a.closers[index](ctx)
		}
		repository.CloseDatabase()
	})
}
