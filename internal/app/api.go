package app

import (
	"fmt"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/pkg/cron"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/internal/service"
)

// NewAPI constructs only the API/control-plane process.
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

func (a *Application) configureAPI(reporter *metrics.Reporter) error {
	return a.configureAPIHTTP(reporter, a.checkMySQLReadiness)
}

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
		return fmt.Errorf("初始化 API HTTP 路由失败: %w", err)
	}
	a.setHTTPServer(handler)
	return nil
}
