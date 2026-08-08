package app

import (
	"context"
	"fmt"

	"github.com/chronoflow/internal/pkg/logger"
	redisstream "github.com/chronoflow/internal/pkg/redis"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type streamDependencies struct {
	client    *goredis.Client
	publisher *redisstream.StreamPublisher
	consumer  *redisstream.StreamConsumer
}

func (a *Application) connectStream() (*streamDependencies, error) {
	client, err := redisstream.InitRedis(a.cfg.Redis.Addr, a.cfg.Redis.Password, a.cfg.Redis.DB)
	if err != nil {
		return nil, fmt.Errorf("initialize Redis: %w", err)
	}
	a.addCloser(func(context.Context) {
		if err := client.Close(); err != nil {
			logger.Warn("Failed to close Redis connection", zap.Error(err))
		}
	})

	return &streamDependencies{
		client:    client,
		publisher: redisstream.NewStreamPublisher(client),
		consumer:  redisstream.NewStreamConsumer(client),
	}, nil
}

func (a *Application) checkStreamReadiness(client *goredis.Client) readinessChecker {
	return func(ctx context.Context) map[string]string {
		failures := a.checkMySQLReadiness(ctx)
		if err := client.Ping(ctx).Err(); err != nil {
			failures["redis"] = err.Error()
		}
		return failures
	}
}
