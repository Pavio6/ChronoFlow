package launcher

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/chronoflow/internal/app"
	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/pkg/logger"
)

// Builder 用于构建一个独立部署的 ChronoFlow 角色
type Builder func(*config.Config) (*app.Application, error)

// Main 加载共享运行配置并启动一个角色
func Main(build Builder) {
	if err := Run(build); err != nil {
		fmt.Fprintf(os.Stderr, "ChronoFlow failed to start: %v\n", err)
		os.Exit(1)
	}
}

// Run 从 Main 中拆出，以保持各角色入口精简且便于测试
func Run(build Builder) error {
	if build == nil {
		return fmt.Errorf("role builder is required")
	}
	cfg, err := config.Load("./config")
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
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
