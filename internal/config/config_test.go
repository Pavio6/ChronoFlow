package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadDefaultsWorkerPoolSizes(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Scheduler.WorkerPoolSize != 16 {
		t.Fatalf("scheduler worker pool size = %d, want 16", cfg.Scheduler.WorkerPoolSize)
	}
	if cfg.Trigger.WorkerPoolSize != 100 {
		t.Fatalf("trigger worker pool size = %d, want 100", cfg.Trigger.WorkerPoolSize)
	}
	if cfg.Monitoring.GrafanaURL != "http://localhost:3001" {
		t.Fatalf("grafana url = %q, want local default", cfg.Monitoring.GrafanaURL)
	}
}

func TestLoadNormalizesInvalidWorkerPoolSizes(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(
		"scheduler:\n  worker_pool_size: 0\ntrigger:\n  worker_pool_size: -1\n",
	), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Scheduler.WorkerPoolSize != 16 {
		t.Fatalf("scheduler worker pool size = %d, want normalized default 16", cfg.Scheduler.WorkerPoolSize)
	}
	if cfg.Trigger.WorkerPoolSize != 100 {
		t.Fatalf("trigger worker pool size = %d, want normalized default 100", cfg.Trigger.WorkerPoolSize)
	}
}
