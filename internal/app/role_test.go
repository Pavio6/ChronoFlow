package app

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseRole(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    Role
		wantErr bool
	}{
		{name: "default", want: RoleAll},
		{name: "api", args: []string{"api"}, want: RoleAPI},
		{name: "scheduler", args: []string{"scheduler"}, want: RoleScheduler},
		{name: "dispatcher", args: []string{"dispatcher"}, want: RoleDispatcher},
		{name: "worker", args: []string{"worker"}, want: RoleWorker},
		{name: "all", args: []string{"all"}, want: RoleAll},
		{name: "case insensitive", args: []string{"WoRkEr"}, want: RoleWorker},
		{name: "unknown", args: []string{"unknown"}, wantErr: true},
		{name: "too many", args: []string{"api", "worker"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRole(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseRole(%v) returned nil error", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRole(%v) returned error: %v", tt.args, err)
			}
			if got != tt.want {
				t.Fatalf("ParseRole(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseRoleHelp(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		_, err := ParseRole([]string{arg})
		if !errors.Is(err, ErrHelp) {
			t.Fatalf("ParseRole(%q) error = %v, want ErrHelp", arg, err)
		}
	}
}

func TestRoleCapabilities(t *testing.T) {
	tests := []struct {
		role       Role
		runsAPI    bool
		mode       string
		components []string
	}{
		{role: RoleAPI, runsAPI: true, mode: "control-plane", components: []string{"api"}},
		{
			role:       RoleScheduler,
			mode:       "scheduler",
			components: []string{"scheduler", "execution-reconciler"},
		},
		{role: RoleDispatcher, mode: "outbox-dispatcher", components: []string{"outbox-dispatcher"}},
		{
			role:       RoleWorker,
			mode:       "stream-worker",
			components: []string{"stream-worker", "stream-retention-cleaner"},
		},
		{
			role:    RoleAll,
			runsAPI: true,
			mode:    "combined",
			components: []string{
				"api",
				"scheduler",
				"execution-reconciler",
				"outbox-dispatcher",
				"stream-worker",
				"stream-retention-cleaner",
			},
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			if got := tt.role.RunsAPI(); got != tt.runsAPI {
				t.Fatalf("RunsAPI() = %t, want %t", got, tt.runsAPI)
			}
			if got := tt.role.RuntimeMode(); got != tt.mode {
				t.Fatalf("RuntimeMode() = %q, want %q", got, tt.mode)
			}
			if got := tt.role.Components(); !reflect.DeepEqual(got, tt.components) {
				t.Fatalf("Components() = %v, want %v", got, tt.components)
			}
		})
	}
}

func TestRoleRedisDependencies(t *testing.T) {
	if roleRequiresRedis(RoleScheduler) {
		t.Fatal("Scheduler must not require Redis")
	}
	if roleRequiresRedis(RoleAPI) {
		t.Fatal("API must not require Redis")
	}
	for _, role := range []Role{RoleDispatcher, RoleWorker, RoleAll} {
		if !roleRequiresRedis(role) {
			t.Fatalf("%s unexpectedly does not require Redis", role)
		}
	}
}
