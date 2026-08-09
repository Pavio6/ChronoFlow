package app

import (
	"reflect"
	"testing"
)

// TestRoleCapabilities 验证对应的测试场景。
func TestRoleCapabilities(t *testing.T) {
	tests := []struct {
		role       Role
		mode       string
		components []string
	}{
		{role: RoleAPI, mode: "control-plane", components: []string{"api"}},
		{role: RoleScheduler, mode: "scheduler", components: []string{"scheduler", "execution-reconciler"}},
		{role: RoleDispatcher, mode: "outbox-dispatcher", components: []string{"outbox-dispatcher"}},
		{role: RoleWorker, mode: "stream-worker", components: []string{"stream-worker", "stream-retention-cleaner"}},
		{
			role: RoleAll,
			mode: "combined",
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
			if got := tt.role.RuntimeMode(); got != tt.mode {
				t.Fatalf("RuntimeMode() = %q, want %q", got, tt.mode)
			}
			if got := tt.role.Components(); !reflect.DeepEqual(got, tt.components) {
				t.Fatalf("Components() = %v, want %v", got, tt.components)
			}
		})
	}
}
