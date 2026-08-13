package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config 应用全局配置
type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	Runtime    RuntimeConfig    `mapstructure:"runtime"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Migrations MigrationConfig  `mapstructure:"migrations"`
	Redis      RedisConfig      `mapstructure:"redis"`
	Scheduler  SchedulerConfig  `mapstructure:"scheduler"`
	Outbox     OutboxConfig     `mapstructure:"outbox"`
	Worker     WorkerConfig     `mapstructure:"worker"`
	Recovery   RecoveryConfig   `mapstructure:"recovery"`
	Security   SecurityConfig   `mapstructure:"security"`
	Monitoring MonitoringConfig `mapstructure:"monitoring"`
	Log        LogConfig        `mapstructure:"log"`
}

// ServerConfig HTTP 服务器配置
type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"` // debug, release, test
}

// RuntimeConfig 配置所有角色共享的进程生命周期行为
type RuntimeConfig struct {
	ShutdownTimeoutSeconds int `mapstructure:"shutdown_timeout_seconds"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	DSN             string `mapstructure:"dsn"`
	MaxOpenConns    int    `mapstructure:"max_open_conns"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetime int    `mapstructure:"conn_max_lifetime"` // 分钟
	LogLevel        string `mapstructure:"log_level"`
}

// MigrationConfig 配置带版本 SQL 迁移的来源
type MigrationConfig struct {
	Path string `mapstructure:"path"`
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// SchedulerConfig 配置以 MySQL 为权威数据源的到期 Timer 扫描器
type SchedulerConfig struct {
	PollIntervalMS      int `mapstructure:"poll_interval_ms"`
	BatchSize           int `mapstructure:"batch_size"`
	MisfireGraceSeconds int `mapstructure:"misfire_grace_seconds"`
	DefaultMaxCatchUp   int `mapstructure:"default_max_catch_up"`
}

// OutboxConfig 配置从 MySQL 到 Redis Stream 的可靠发布
type OutboxConfig struct {
	PollIntervalMS    int    `mapstructure:"poll_interval_ms"`
	BatchSize         int    `mapstructure:"batch_size"`
	ClaimTTLSeconds   int    `mapstructure:"claim_ttl_seconds"`
	MaxBackoffSeconds int    `mapstructure:"max_backoff_seconds"`
	Stream            string `mapstructure:"stream"`
	ConsumerGroup     string `mapstructure:"consumer_group"`
	StreamMaxLen      int64  `mapstructure:"stream_max_len"`
}

// WorkerConfig 配置 Redis Stream 消费和回调执行行为
type WorkerConfig struct {
	PoolSize               int   `mapstructure:"pool_size"`
	ReadCount              int64 `mapstructure:"read_count"`
	ReadBlockMS            int   `mapstructure:"read_block_ms"`
	LeaseTTLSeconds        int   `mapstructure:"lease_ttl_seconds"`
	HeartbeatSeconds       int   `mapstructure:"heartbeat_seconds"`
	ReclaimIdleSeconds     int   `mapstructure:"reclaim_idle_seconds"`
	ReclaimIntervalSeconds int   `mapstructure:"reclaim_interval_seconds"`
	HTTPTimeoutSeconds     int   `mapstructure:"http_timeout_seconds"`
	MaxResponseBytes       int64 `mapstructure:"max_response_bytes"`
	RetryBaseSeconds       int   `mapstructure:"retry_base_seconds"`
	RetryMaxSeconds        int   `mapstructure:"retry_max_seconds"`
}

// RecoveryConfig 配置持久化执行修复和保留数据清理行为
type RecoveryConfig struct {
	Enabled                bool `mapstructure:"enabled"`
	ScanIntervalSeconds    int  `mapstructure:"scan_interval_seconds"`
	BatchSize              int  `mapstructure:"batch_size"`
	PendingStaleSeconds    int  `mapstructure:"pending_stale_seconds"`
	CleanupIntervalMinutes int  `mapstructure:"cleanup_interval_minutes"`
	OutboxRetentionDays    int  `mapstructure:"outbox_retention_days"`
	ExecutionRetentionDays int  `mapstructure:"execution_retention_days"`
	StreamRetentionHours   int  `mapstructure:"stream_retention_hours"`
}

// SecurityConfig 包含部署时 API 与回调安全的基础配置
type SecurityConfig struct {
	APIKey                string   `mapstructure:"api_key"`
	AllowedOrigins        []string `mapstructure:"allowed_origins"`
	AllowPrivateCallbacks bool     `mapstructure:"allow_private_callbacks"`
	MaxRequestBytes       int64    `mapstructure:"max_request_bytes"`
}

// MonitoringConfig 配置健康指标的周期采集
type MonitoringConfig struct {
	PrometheusURL string `mapstructure:"prometheus_url"`
	GrafanaURL    string `mapstructure:"grafana_url"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level    string `mapstructure:"level"`
	Format   string `mapstructure:"format"`    // json, console
	Output   string `mapstructure:"output"`    // stdout, file
	FilePath string `mapstructure:"file_path"` // 文件输出路径
}

// AppConfig 全局配置实例
var AppConfig *Config

// Load 加载配置文件
func Load(configPath string) (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(configPath)

	// 设置默认值
	setDefaults()

	// 读取 CHRONOFLOW_* 环境变量；嵌套配置中的点使用下划线
	viper.SetEnvPrefix("CHRONOFLOW")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	}

	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	normalizeSchedulerConfig(&cfg.Scheduler)
	normalizeOutboxConfig(&cfg.Outbox)
	normalizeWorkerConfig(&cfg.Worker)
	normalizeRecoveryConfig(&cfg.Recovery)
	normalizeSecurityConfig(&cfg.Security)
	normalizeMonitoringConfig(&cfg.Monitoring)
	normalizeRuntimeConfig(&cfg.Runtime)
	normalizeMigrationConfig(&cfg.Migrations)
	AppConfig = cfg
	return cfg, nil
}

// normalizeRuntimeConfig 修正进程生命周期配置中的无效值
func normalizeRuntimeConfig(cfg *RuntimeConfig) {
	if cfg.ShutdownTimeoutSeconds < 1 {
		cfg.ShutdownTimeoutSeconds = 15
	}
}

// normalizeMigrationConfig 为迁移目录设置默认值
func normalizeMigrationConfig(cfg *MigrationConfig) {
	if strings.TrimSpace(cfg.Path) == "" {
		cfg.Path = "migrations"
	}
}

// normalizeSchedulerConfig 修正 Scheduler 配置中的无效值
func normalizeSchedulerConfig(cfg *SchedulerConfig) {
	if cfg.PollIntervalMS < 10 {
		cfg.PollIntervalMS = 500
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = 100
	}
	if cfg.MisfireGraceSeconds < 0 {
		cfg.MisfireGraceSeconds = 0
	}
	if cfg.DefaultMaxCatchUp < 1 {
		cfg.DefaultMaxCatchUp = 10
	}
}

// normalizeOutboxConfig 修正 Outbox 配置中的无效值
func normalizeOutboxConfig(cfg *OutboxConfig) {
	if cfg.PollIntervalMS < 10 {
		cfg.PollIntervalMS = 200
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = 100
	}
	if cfg.ClaimTTLSeconds < 1 {
		cfg.ClaimTTLSeconds = 30
	}
	if cfg.MaxBackoffSeconds < 1 {
		cfg.MaxBackoffSeconds = 30
	}
	if cfg.Stream == "" {
		cfg.Stream = "chronoflow:execution:ready"
	}
	if cfg.ConsumerGroup == "" {
		cfg.ConsumerGroup = "chronoflow-workers"
	}
	if cfg.StreamMaxLen < 0 {
		cfg.StreamMaxLen = 0
	}
}

// normalizeWorkerConfig 修正 Worker 配置中的无效值并保持各时间参数一致
func normalizeWorkerConfig(cfg *WorkerConfig) {
	if cfg.PoolSize < 1 {
		cfg.PoolSize = 100
	}
	if cfg.ReadCount < 1 {
		cfg.ReadCount = 20
	}
	if cfg.ReadBlockMS < 100 {
		cfg.ReadBlockMS = 2000
	}
	if cfg.LeaseTTLSeconds < 3 {
		cfg.LeaseTTLSeconds = 30
	}
	if cfg.HeartbeatSeconds < 1 ||
		cfg.HeartbeatSeconds*2 >= cfg.LeaseTTLSeconds {
		cfg.HeartbeatSeconds = cfg.LeaseTTLSeconds / 3
	}
	if cfg.ReclaimIdleSeconds < cfg.LeaseTTLSeconds {
		cfg.ReclaimIdleSeconds = cfg.LeaseTTLSeconds
	}
	if cfg.ReclaimIntervalSeconds < 1 {
		cfg.ReclaimIntervalSeconds = 10
	}
	if cfg.HTTPTimeoutSeconds < 1 {
		cfg.HTTPTimeoutSeconds = 12
	}
	if cfg.MaxResponseBytes < 1 {
		cfg.MaxResponseBytes = 1024 * 1024
	}
	if cfg.RetryBaseSeconds < 1 {
		cfg.RetryBaseSeconds = 2
	}
	if cfg.RetryMaxSeconds < cfg.RetryBaseSeconds {
		cfg.RetryMaxSeconds = 60
	}
}

// normalizeRecoveryConfig 修正执行恢复和清理配置中的无效值
func normalizeRecoveryConfig(cfg *RecoveryConfig) {
	if cfg.ScanIntervalSeconds < 1 {
		cfg.ScanIntervalSeconds = 10
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = 100
	}
	if cfg.PendingStaleSeconds < 1 {
		cfg.PendingStaleSeconds = 30
	}
	if cfg.CleanupIntervalMinutes < 1 {
		cfg.CleanupIntervalMinutes = 60
	}
	if cfg.OutboxRetentionDays < 1 {
		cfg.OutboxRetentionDays = 7
	}
	if cfg.ExecutionRetentionDays < cfg.OutboxRetentionDays {
		cfg.ExecutionRetentionDays = 30
	}
	if cfg.StreamRetentionHours < 1 {
		cfg.StreamRetentionHours = 24
	}
}

// normalizeSecurityConfig 补齐安全配置的默认值
func normalizeSecurityConfig(cfg *SecurityConfig) {
	if len(cfg.AllowedOrigins) == 0 {
		cfg.AllowedOrigins = []string{"http://localhost:3000"}
	}
	if cfg.MaxRequestBytes < 1 {
		cfg.MaxRequestBytes = 1024 * 1024
	}
}

// normalizeMonitoringConfig 补齐监控服务地址的默认值
func normalizeMonitoringConfig(cfg *MonitoringConfig) {
	if cfg.PrometheusURL == "" {
		cfg.PrometheusURL = "http://localhost:9090"
	}
	if cfg.GrafanaURL == "" {
		cfg.GrafanaURL = "http://localhost:3001"
	}
}

// setDefaults 设置配置默认值
func setDefaults() {
	// 服务器默认配置
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.mode", "debug")

	// 进程生命周期默认配置
	viper.SetDefault("runtime.shutdown_timeout_seconds", 15)

	// 数据库默认配置
	viper.SetDefault("database.max_open_conns", 100)
	viper.SetDefault("database.max_idle_conns", 10)
	viper.SetDefault("database.conn_max_lifetime", 60)
	viper.SetDefault("database.log_level", "warn")
	viper.SetDefault("migrations.path", "migrations")

	// Redis 默认配置
	viper.SetDefault("redis.addr", "127.0.0.1:6379")
	viper.SetDefault("redis.password", "")
	viper.SetDefault("redis.db", 0)

	// 调度器默认配置
	viper.SetDefault("scheduler.poll_interval_ms", 500)
	viper.SetDefault("scheduler.batch_size", 100)
	viper.SetDefault("scheduler.misfire_grace_seconds", 30)
	viper.SetDefault("scheduler.default_max_catch_up", 10)

	// 事务 Outbox 与 Redis Stream
	viper.SetDefault("outbox.poll_interval_ms", 200)
	viper.SetDefault("outbox.batch_size", 100)
	viper.SetDefault("outbox.claim_ttl_seconds", 30)
	viper.SetDefault("outbox.max_backoff_seconds", 30)
	viper.SetDefault("outbox.stream", "chronoflow:execution:ready")
	viper.SetDefault("outbox.consumer_group", "chronoflow-workers")
	viper.SetDefault("outbox.stream_max_len", 0)

	// Redis Stream Worker
	viper.SetDefault("worker.pool_size", 100)
	viper.SetDefault("worker.read_count", 20)
	viper.SetDefault("worker.read_block_ms", 2000)
	viper.SetDefault("worker.lease_ttl_seconds", 30)
	viper.SetDefault("worker.heartbeat_seconds", 10)
	viper.SetDefault("worker.reclaim_idle_seconds", 30)
	viper.SetDefault("worker.reclaim_interval_seconds", 10)
	viper.SetDefault("worker.http_timeout_seconds", 12)
	viper.SetDefault("worker.max_response_bytes", 1024*1024)
	viper.SetDefault("worker.retry_base_seconds", 2)
	viper.SetDefault("worker.retry_max_seconds", 60)

	// Reconciler 与保留数据清理
	viper.SetDefault("recovery.enabled", true)
	viper.SetDefault("recovery.scan_interval_seconds", 10)
	viper.SetDefault("recovery.batch_size", 100)
	viper.SetDefault("recovery.pending_stale_seconds", 30)
	viper.SetDefault("recovery.cleanup_interval_minutes", 60)
	viper.SetDefault("recovery.outbox_retention_days", 7)
	viper.SetDefault("recovery.execution_retention_days", 30)
	viper.SetDefault("recovery.stream_retention_hours", 24)

	// API 与回调安全基础配置
	viper.SetDefault("security.api_key", "")
	viper.SetDefault("security.allowed_origins", []string{"http://localhost:3000"})
	viper.SetDefault("security.allow_private_callbacks", false)
	viper.SetDefault("security.max_request_bytes", 1024*1024)

	// 可观测性采集默认配置
	viper.SetDefault("monitoring.prometheus_url", "http://localhost:9090")
	viper.SetDefault("monitoring.grafana_url", "http://localhost:3001")

	// 日志默认配置
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", "json")
	viper.SetDefault("log.output", "stdout")
}
