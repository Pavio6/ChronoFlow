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

// Result is the observable result of a migration command.
type Result struct {
	Version uint
	Dirty   bool
	Changed bool
}

// Execute runs a golang-migrate operation against the configured MySQL DSN.
// The MySQL driver creates and maintains schema_migrations and serializes
// concurrent migration attempts with its database lock.
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

func mysqlURL(dsn string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(dsn)), "mysql://") {
		return dsn
	}
	return "mysql://" + dsn
}

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
