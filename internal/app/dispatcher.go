package app

import (
	"context"
	"fmt"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/pkg/metrics"
	redisstream "github.com/chronoflow/internal/pkg/redis"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/internal/service"
)

// NewDispatcher 创建只负责发布 Transactional Outbox 的 Dispatcher 进程
func NewDispatcher(cfg *config.Config) (*Application, error) {
	application, err := newApplication(cfg, RoleDispatcher)
	if err != nil {
		return nil, err
	}
	stream, err := application.connectStream()
	if err != nil {
		return application.fail(err)
	}
	reporter := metrics.NewReporter()
	if err := application.configureDispatcher(reporter, stream.publisher); err != nil {
		return application.fail(err)
	}
	application.setHTTPServer(newOperationalHTTPHandler(
		cfg,
		RoleDispatcher,
		reporter,
		application.checkStreamReadiness(stream.client),
	))
	return application, nil
}

// configureDispatcher 初始化 Consumer Group 并注册 Outbox Dispatcher 后台服务
func (a *Application) configureDispatcher(reporter *metrics.Reporter, publisher *redisstream.StreamPublisher) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := publisher.EnsureConsumerGroup(ctx, a.cfg.Outbox.Stream, a.cfg.Outbox.ConsumerGroup); err != nil {
		return fmt.Errorf("initialize Redis Stream consumer group: %w", err)
	}

	dispatcher := service.NewOutboxDispatcher(
		repository.NewOutboxRepository(repository.DB),
		publisher,
		reporter,
		&a.cfg.Outbox,
		a.instanceID(),
	)
	a.background = append(a.background, backgroundService{name: "outbox-dispatcher", run: dispatcher.Start})
	return nil
}
