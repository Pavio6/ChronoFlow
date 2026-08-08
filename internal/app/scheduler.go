package app

import (
	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/pkg/cron"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/internal/service"
)

// NewScheduler constructs only the authoritative scheduler process.
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
