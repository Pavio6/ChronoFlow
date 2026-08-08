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
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/pkg/logger"
	"go.uber.org/zap"
)

type backgroundService struct {
	name string
	run  func(context.Context)
}

// Application owns the lifecycle and only the resources of one deployable role.
type Application struct {
	cfg        *config.Config
	role       Role
	server     *http.Server
	sqlDB      *sql.DB
	background []backgroundService
	closers    []func(context.Context)
	closeOnce  sync.Once
}

func newApplication(cfg *config.Config, role Role) (*Application, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

	application := &Application{cfg: cfg, role: role}
	if err := repository.InitDatabase(&cfg.Database); err != nil {
		return application.fail(fmt.Errorf("初始化数据库失败: %w", err))
	}
	sqlDB, err := repository.DB.DB()
	if err != nil {
		return application.fail(fmt.Errorf("获取数据库连接失败: %w", err))
	}
	application.sqlDB = sqlDB
	return application, nil
}

func (a *Application) fail(err error) (*Application, error) {
	shutdownTimeout := time.Duration(a.cfg.Runtime.ShutdownTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	a.closeWithContext(ctx)
	return nil, err
}

func (a *Application) setHTTPServer(handler http.Handler) {
	a.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", a.cfg.Server.Port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func (a *Application) addCloser(closer func(context.Context)) {
	a.closers = append(a.closers, closer)
}

func (a *Application) instanceID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s-%s-%d", a.role, hostname, os.Getpid())
}

// Run starts one independently deployable role and blocks until it stops.
func (a *Application) Run(ctx context.Context) error {
	if a.server == nil {
		return errors.New("HTTP server is not configured")
	}

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
		logger.Info("收到关闭信号，开始优雅关闭", zap.String("role", string(a.role)))
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

// Close releases process-local resources for the role.
func (a *Application) Close() {
	timeout := time.Duration(a.cfg.Runtime.ShutdownTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	a.closeWithContext(ctx)
}

func (a *Application) closeWithContext(ctx context.Context) {
	a.closeOnce.Do(func() {
		for index := len(a.closers) - 1; index >= 0; index-- {
			a.closers[index](ctx)
		}
		repository.CloseDatabase()
	})
}
