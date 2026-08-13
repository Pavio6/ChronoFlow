package migration

import (
	"errors"
	"fmt"
	"io"

	"github.com/chronoflow/internal/config"
)

// RunCLI 使用项目配置执行发布阶段的迁移命令
func RunCLI(args []string, output io.Writer) error {
	command, err := ParseCommand(args)
	if errors.Is(err, ErrHelp) {
		_, _ = fmt.Fprint(output, Usage())
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w\n\n%s", err, Usage())
	}

	cfg, err := config.Load("./config")
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	result, err := Execute(cfg.Database.DSN, cfg.Migrations.Path, command)
	if err != nil {
		return err
	}
	if result.Dirty {
		return fmt.Errorf("migration version %d is dirty; verify the database before using migrate force", result.Version)
	}
	if command.Action == ActionVersion {
		_, _ = fmt.Fprintf(output, "migration version: %d (clean)\n", result.Version)
		return nil
	}
	if result.Changed {
		_, _ = fmt.Fprintf(output, "migration %s completed at version %d\n", command.Action, result.Version)
		return nil
	}
	_, _ = fmt.Fprintf(output, "migration is already at version %d\n", result.Version)
	return nil
}
