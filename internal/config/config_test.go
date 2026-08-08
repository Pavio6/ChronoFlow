package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Monitoring.GrafanaURL != "http://localhost:3001" {
		t.Fatalf("grafana url = %q, want local default", cfg.Monitoring.GrafanaURL)
	}
	if cfg.Runtime.ShutdownTimeoutSeconds != 15 {
		t.Fatalf("runtime shutdown timeout = %d, want 15", cfg.Runtime.ShutdownTimeoutSeconds)
	}
	if cfg.Migrations.Path != "migrations" {
		t.Fatalf("migration path = %q, want migrations", cfg.Migrations.Path)
	}
	if cfg.Scheduler.BatchSize != 100 ||
		cfg.Scheduler.PollIntervalMS != 500 {
		t.Fatalf("scheduler defaults = %+v", cfg.Scheduler)
	}
	if cfg.Outbox.Stream != "chronoflow:execution:ready" {
		t.Fatalf("outbox stream = %q, want default stream", cfg.Outbox.Stream)
	}
	if cfg.Outbox.ConsumerGroup != "chronoflow-workers" {
		t.Fatalf("outbox consumer group = %q, want default group", cfg.Outbox.ConsumerGroup)
	}
	if cfg.Worker.PoolSize != 100 || cfg.Worker.LeaseTTLSeconds != 30 {
		t.Fatalf("worker defaults = %+v", cfg.Worker)
	}
	if !cfg.Recovery.Enabled || cfg.Recovery.BatchSize != 100 {
		t.Fatalf("recovery defaults = %+v", cfg.Recovery)
	}
	if len(cfg.Security.AllowedOrigins) != 1 ||
		cfg.Security.AllowedOrigins[0] != "http://localhost:3000" {
		t.Fatalf("allowed origins = %v", cfg.Security.AllowedOrigins)
	}
}

func TestLoadNormalizesInvalidConcurrencyAndLeaseValues(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(
		"scheduler:\n  batch_size: 0\n  poll_interval_ms: 0\n"+
			"worker:\n  pool_size: 0\n  lease_ttl_seconds: 1\n  heartbeat_seconds: 99\n",
	), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Scheduler.BatchSize != 100 ||
		cfg.Scheduler.PollIntervalMS != 500 {
		t.Fatalf("normalized scheduler config = %+v", cfg.Scheduler)
	}
	if cfg.Worker.PoolSize != 100 ||
		cfg.Worker.LeaseTTLSeconds != 30 ||
		cfg.Worker.HeartbeatSeconds != 10 {
		t.Fatalf("normalized worker config = %+v", cfg.Worker)
	}
}

func TestLoadSupportsPrefixedEnvironmentOverrides(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("CHRONOFLOW_SERVER_PORT", "18081")
	t.Setenv("CHRONOFLOW_RUNTIME_SHUTDOWN_TIMEOUT_SECONDS", "21")

	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Server.Port != 18081 {
		t.Fatalf("server port = %d, want environment override 18081", cfg.Server.Port)
	}
	if cfg.Runtime.ShutdownTimeoutSeconds != 21 {
		t.Fatalf("shutdown timeout = %d, want environment override 21", cfg.Runtime.ShutdownTimeoutSeconds)
	}
}
