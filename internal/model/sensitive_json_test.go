package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSensitiveCallbackDataIsNotSerialized 验证对应的测试场景
func TestSensitiveCallbackDataIsNotSerialized(t *testing.T) {
	executionJSON, err := json.Marshal(TimerExecution{
		ID:              1,
		LeaseOwner:      "worker-private-host",
		RunToken:        "private-run-token",
		RequestSnapshot: `{"headers":{"Authorization":"Bearer secret"}}`,
	})
	if err != nil {
		t.Fatalf("marshal execution: %v", err)
	}
	if strings.Contains(string(executionJSON), "request_snapshot") ||
		strings.Contains(string(executionJSON), "secret") ||
		strings.Contains(string(executionJSON), "worker-private-host") ||
		strings.Contains(string(executionJSON), "private-run-token") {
		t.Fatalf("execution JSON exposed internal state: %s", executionJSON)
	}

	timerJSON, err := json.Marshal(TimerDefinition{
		ID:              1,
		CallbackHeaders: `{"Authorization":"Bearer secret"}`,
	})
	if err != nil {
		t.Fatalf("marshal timer: %v", err)
	}
	if strings.Contains(string(timerJSON), "callback_headers") ||
		strings.Contains(string(timerJSON), "secret") {
		t.Fatalf("timer JSON exposed callback headers: %s", timerJSON)
	}
}
