package launcher

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/chronoflow/internal/app"
	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/pkg/logger"
)

// Builder constructs one independently deployed ChronoFlow role.
type Builder func(*config.Config) (*app.Application, error)

// Main loads shared runtime configuration and starts exactly one role.
func Main(build Builder) {
	if err := Run(build); err != nil {
		fmt.Fprintf(os.Stderr, "ChronoFlow 启动失败: %v\n", err)
		os.Exit(1)
	}
}

// Run is separated from Main to keep every role entrypoint thin and testable.
func Run(build Builder) error {
	if build == nil {
		return fmt.Errorf("role builder is required")
	}
	cfg, err := config.Load("./config")
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	logger.Init(cfg.Log.Level, cfg.Log.Format, cfg.Log.Output, cfg.Log.FilePath)
	defer logger.Sync()

	application, err := build(cfg)
	if err != nil {
		return err
	}
	defer application.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return application.Run(ctx)
}
