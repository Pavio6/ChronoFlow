package app

import (
	"context"
	"fmt"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/pkg/pool"
	redisstream "github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/internal/service"
	"github.com/chronoflow/pkg/logger"
	"go.uber.org/zap"
)

// NewWorker constructs only the Redis Stream callback execution process.
func NewWorker(cfg *config.Config) (*Application, error) {
	application, err := newApplication(cfg, RoleWorker)
	if err != nil {
		return nil, err
	}
	stream, err := application.connectStream()
	if err != nil {
		return application.fail(err)
	}
	reporter := metrics.NewReporter()
	if err := application.configureWorker(reporter, stream.publisher, stream.consumer); err != nil {
		return application.fail(err)
	}
	application.setHTTPServer(newOperationalHTTPHandler(
		cfg,
		RoleWorker,
		reporter,
		application.checkStreamReadiness(stream.client),
	))
	return application, nil
}

func (a *Application) configureWorker(
	reporter *metrics.Reporter,
	publisher *redisstream.StreamPublisher,
	consumer *redisstream.StreamConsumer,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := publisher.EnsureConsumerGroup(ctx, a.cfg.Outbox.Stream, a.cfg.Outbox.ConsumerGroup); err != nil {
		return fmt.Errorf("初始化 Redis Stream Consumer Group 失败: %w", err)
	}

	workerPool, err := pool.NewGoWorkerPool(a.cfg.Worker.PoolSize)
	if err != nil {
		return fmt.Errorf("创建 Worker ants Pool 失败: %w", err)
	}
	a.addCloser(func(ctx context.Context) {
		if err := workerPool.ReleaseContext(ctx); err != nil {
			logger.Warn("协程池未在超时时间内停止", zap.Error(err))
		}
	})

	executionRepo := repository.NewTimerExecutionRepository(repository.DB)
	worker := service.NewStreamWorker(
		executionRepo,
		consumer,
		workerPool,
		service.NewConfiguredCallbackClient(&a.cfg.Worker, &a.cfg.Security),
		reporter,
		&a.cfg.Worker,
		&a.cfg.Outbox,
		a.instanceID(),
	)
	cleaner := service.NewStreamRetentionCleaner(consumer, reporter, &a.cfg.Outbox, &a.cfg.Recovery)
	a.background = append(a.background,
		backgroundService{name: "stream-worker", run: worker.Start},
		backgroundService{name: "stream-retention-cleaner", run: cleaner.Start},
	)
	return nil
}
