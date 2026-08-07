package app

import (
	"errors"
	"fmt"
	"strings"
)

// Role identifies the independently deployable process role.
type Role string

const (
	RoleAPI        Role = "api"
	RoleScheduler  Role = "scheduler"
	RoleDispatcher Role = "dispatcher"
	RoleWorker     Role = "worker"
	RoleAll        Role = "all"
)

// ErrHelp signals that command usage should be printed without treating it as a failure.
var ErrHelp = errors.New("help requested")

var validRoles = []Role{
	RoleAPI,
	RoleScheduler,
	RoleDispatcher,
	RoleWorker,
	RoleAll,
}

// ParseRole parses the optional positional role argument.
// Omitting the argument starts all components for local development.
func ParseRole(args []string) (Role, error) {
	if len(args) == 0 {
		return RoleAll, nil
	}
	if len(args) != 1 {
		return "", fmt.Errorf("expected at most one role argument, got %d", len(args))
	}

	switch args[0] {
	case "-h", "--help", "help":
		return "", ErrHelp
	}

	role := Role(strings.ToLower(strings.TrimSpace(args[0])))
	if role.IsValid() {
		return role, nil
	}
	return "", fmt.Errorf("unknown role %q", args[0])
}

// Usage returns command help for the single ChronoFlow binary.
func Usage() string {
	return `Usage:
  chronoflow [api|scheduler|dispatcher|worker|all]

Roles:
  api         HTTP API, management UI and operational endpoints
  scheduler   MySQL authoritative scheduler and execution reconciler
  dispatcher  Transactional Outbox to Redis Streams publisher
  worker      Redis Streams consumer, MySQL Lease owner and callback executor
  all         All currently implemented components for local development (default)
`
}

// IsValid reports whether r is a supported role.
func (r Role) IsValid() bool {
	for _, candidate := range validRoles {
		if r == candidate {
			return true
		}
	}
	return false
}

// RunsAPI reports whether this role exposes control-plane and management routes.
func (r Role) RunsAPI() bool {
	return r == RoleAPI || r == RoleAll
}

// RuntimeMode describes the implementation status exposed by health endpoints.
func (r Role) RuntimeMode() string {
	switch r {
	case RoleAPI:
		return "control-plane"
	case RoleScheduler:
		return "scheduler"
	case RoleDispatcher:
		return "outbox-dispatcher"
	case RoleWorker:
		return "stream-worker"
	case RoleAll:
		return "combined"
	default:
		return "unknown"
	}
}

// Components returns the runtime components owned by the role.
func (r Role) Components() []string {
	switch r {
	case RoleAPI:
		return []string{"api"}
	case RoleScheduler:
		return []string{"scheduler", "execution-reconciler"}
	case RoleDispatcher:
		return []string{"outbox-dispatcher"}
	case RoleWorker:
		return []string{"stream-worker", "stream-retention-cleaner"}
	case RoleAll:
		return []string{
			"api",
			"scheduler",
			"execution-reconciler",
			"outbox-dispatcher",
			"stream-worker",
			"stream-retention-cleaner",
		}
	default:
		return nil
	}
}
