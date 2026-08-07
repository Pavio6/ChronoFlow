package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/chronoflow/internal/app"
	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/pkg/logger"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "ChronoFlow 启动失败: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	role, err := app.ParseRole(args)
	if errors.Is(err, app.ErrHelp) {
		fmt.Print(app.Usage())
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w\n\n%s", err, app.Usage())
	}

	cfg, err := config.Load("./config")
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	logger.Init(cfg.Log.Level, cfg.Log.Format, cfg.Log.Output, cfg.Log.FilePath)
	defer logger.Sync()

	application, err := app.New(cfg, role)
	if err != nil {
		return err
	}
	defer application.Close()

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	return application.Run(ctx)
}
