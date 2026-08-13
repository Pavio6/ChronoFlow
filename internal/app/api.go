package app

import (
	"fmt"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/pkg/cron"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/internal/service"
)

// NewAPI 创建只承载控制面的 API 进程
func NewAPI(cfg *config.Config) (*Application, error) {
	application, err := newApplication(cfg, RoleAPI)
	if err != nil {
		return nil, err
	}
	if err := application.configureAPI(metrics.NewReporter()); err != nil {
		return application.fail(err)
	}
	return application, nil
}

// configureAPI 配置 API 角色所需的 HTTP 服务
func (a *Application) configureAPI(reporter *metrics.Reporter) error {
	return a.configureAPIHTTP(reporter, a.checkMySQLReadiness)
}

// configureAPIHTTP 创建 API 路由及其依赖
func (a *Application) configureAPIHTTP(reporter *metrics.Reporter, checkReady readinessChecker) error {
	definitionRepo := repository.NewTimerDefinitionRepository(repository.DB)
	apiDeps := &apiDependencies{
		timerService: service.NewTimerService(
			definitionRepo,
			cron.NewCronParser(),
			&a.cfg.Scheduler,
			&a.cfg.Security,
		),
		defRepo:       definitionRepo,
		executionRepo: repository.NewExecutionQueryRepository(repository.DB),
	}
	handler, err := newAPIHTTPHandler(
		a.cfg,
		a.role,
		reporter,
		checkReady,
		apiDeps,
	)
	if err != nil {
		return fmt.Errorf("initialize API HTTP routes: %w", err)
	}
	a.setHTTPServer(handler)
	return nil
}
