package migration

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// Result 表示一次迁移命令的可观察结果。
type Result struct {
	Version uint
	Dirty   bool
	Changed bool
}

// Execute 通过配置的 MySQL DSN 执行 golang-migrate 操作。
// MySQL 驱动负责维护 schema_migrations，并通过数据库锁串行化并发迁移。
func Execute(dsn, migrationPath string, command Command) (result Result, err error) {
	if strings.TrimSpace(dsn) == "" {
		return result, errors.New("database dsn is required")
	}

	sourceURL, err := fileSourceURL(migrationPath)
	if err != nil {
		return result, err
	}

	m, err := migrate.New(sourceURL, mysqlURL(dsn))
	if err != nil {
		return result, fmt.Errorf("initialize migration engine: %w", err)
	}
	defer func() {
		sourceErr, databaseErr := m.Close()
		if err == nil && sourceErr != nil {
			err = fmt.Errorf("close migration source: %w", sourceErr)
		}
		if err == nil && databaseErr != nil {
			err = fmt.Errorf("close migration database: %w", databaseErr)
		}
	}()

	switch command.Action {
	case ActionUp:
		err = m.Up()
	case ActionDown:
		err = m.Steps(-command.Version)
	case ActionForce:
		err = m.Force(command.Version)
	case ActionVersion:
		result.Version, result.Dirty, err = m.Version()
		if errors.Is(err, migrate.ErrNilVersion) {
			return result, nil
		}
		return result, err
	default:
		return result, fmt.Errorf("unsupported migration action %q", command.Action)
	}

	if errors.Is(err, migrate.ErrNoChange) {
		err = nil
	} else if err != nil {
		return result, fmt.Errorf("run migration %s: %w", command.Action, err)
	} else {
		result.Changed = command.Action != ActionVersion
	}

	result.Version, result.Dirty, err = m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read migration version: %w", err)
	}
	return result, nil
}

// mysqlURL 将普通 MySQL DSN 转换为 golang-migrate 所需的 URL 格式。
func mysqlURL(dsn string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(dsn)), "mysql://") {
		return dsn
	}
	return "mysql://" + dsn
}

// fileSourceURL 将迁移目录解析为 golang-migrate 所需的文件 URL。
func fileSourceURL(migrationPath string) (string, error) {
	path := strings.TrimSpace(migrationPath)
	if path == "" {
		return "", errors.New("migration path is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve migration path: %w", err)
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absPath)}).String(), nil
}
