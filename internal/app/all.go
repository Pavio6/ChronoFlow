package app

import (
	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/pkg/metrics"
)

// NewAll composes every role into one process for local development only.
func NewAll(cfg *config.Config) (*Application, error) {
	application, err := newApplication(cfg, RoleAll)
	if err != nil {
		return nil, err
	}

	reporter := metrics.NewReporter()
	stream, err := application.connectStream()
	if err != nil {
		return application.fail(err)
	}
	if err := application.configureAPIHTTP(reporter, application.checkStreamReadiness(stream.client)); err != nil {
		return application.fail(err)
	}
	application.configureScheduler(reporter)
	if err := application.configureDispatcher(reporter, stream.publisher); err != nil {
		return application.fail(err)
	}
	if err := application.configureWorker(reporter, stream.publisher, stream.consumer); err != nil {
		return application.fail(err)
	}

	return application, nil
}
