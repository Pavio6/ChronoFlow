package migration

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Action identifies the migration operation requested by the CLI.
type Action string

const (
	ActionUp      Action = "up"
	ActionDown    Action = "down"
	ActionVersion Action = "version"
	ActionForce   Action = "force"
)

// Command is intentionally small: golang-migrate owns migration discovery,
// ordering, locking, dirty-state tracking, and version persistence.
type Command struct {
	Action  Action
	Version int
}

// ErrHelp means the caller should print migration-specific usage.
var ErrHelp = errors.New("migration help requested")

// ParseCommand parses the arguments after `chronoflow migrate`.
func ParseCommand(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("missing migration action")
	}

	action := strings.ToLower(strings.TrimSpace(args[0]))
	switch action {
	case "help", "-h", "--help":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("help does not accept additional arguments")
		}
		return Command{}, ErrHelp
	case string(ActionUp), string(ActionVersion):
		if len(args) != 1 {
			return Command{}, fmt.Errorf("migration action %q does not accept additional arguments", action)
		}
		return Command{Action: Action(action)}, nil
	case string(ActionDown), string(ActionForce):
		if len(args) != 2 {
			return Command{}, fmt.Errorf("migration action %q requires exactly one version or step count", action)
		}
		version, err := positiveInt(args[1])
		if err != nil {
			return Command{}, fmt.Errorf("invalid value for migration action %q: %w", action, err)
		}
		return Command{Action: Action(action), Version: version}, nil
	default:
		return Command{}, fmt.Errorf("unknown migration action %q", args[0])
	}
}

func positiveInt(value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, errors.New("must be a positive integer")
	}
	return parsed, nil
}

// Usage returns the migration command help text.
func Usage() string {
	return `Usage:
  chronoflow migrate up
  chronoflow migrate down <steps>
  chronoflow migrate version
  chronoflow migrate force <version>

Actions:
  up               Apply every pending versioned SQL migration
  down <steps>     Revert exactly this many migrations (destructive)
  version          Print the applied version and dirty flag
  force <version>  Repair a known dirty version after manual verification
`
}
