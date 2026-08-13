package app

import (
	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/pkg/cron"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/internal/service"
)

// NewScheduler 创建只负责权威调度与执行修复的 Scheduler 进程
func NewScheduler(cfg *config.Config) (*Application, error) {
	application, err := newApplication(cfg, RoleScheduler)
	if err != nil {
		return nil, err
	}
	reporter := metrics.NewReporter()
	application.configureScheduler(reporter)
	application.setHTTPServer(newOperationalHTTPHandler(cfg, RoleScheduler, reporter, application.checkMySQLReadiness))
	return application, nil
}

// configureScheduler 注册 Scheduler 与 Reconciler 后台服务
func (a *Application) configureScheduler(reporter *metrics.Reporter) {
	cronParser := cron.NewCronParser()
	scheduler := service.NewScheduler(
		repository.NewDueTimerRepository(repository.DB),
		cronParser,
		reporter,
		&a.cfg.Scheduler,
	)
	reconciler := service.NewReconciler(
		repository.NewRecoveryRepository(repository.DB),
		repository.NewTimerExecutionRepository(repository.DB),
		reporter,
		&a.cfg.Recovery,
	)
	a.background = append(a.background,
		backgroundService{name: "scheduler", run: scheduler.Start},
		backgroundService{name: "execution-reconciler", run: reconciler.Start},
	)
}
