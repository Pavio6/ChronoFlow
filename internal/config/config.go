package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config 应用全局配置
type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Redis      RedisConfig      `mapstructure:"redis"`
	Scheduler  SchedulerConfig  `mapstructure:"scheduler"`
	Executor   ExecutorConfig   `mapstructure:"executor"`
	Monitoring MonitoringConfig `mapstructure:"monitoring"`
	Log        LogConfig        `mapstructure:"log"`
}

// ServerConfig HTTP 服务器配置
type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"` // debug, release, test
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Driver          string `mapstructure:"driver"`
	DSN             string `mapstructure:"dsn"`
	MaxOpenConns    int    `mapstructure:"max_open_conns"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetime int    `mapstructure:"conn_max_lifetime"` // 分钟
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	PoolSize int    `mapstructure:"pool_size"`
}

// SchedulerConfig 调度器配置
// migrate_step_minutes: Migrator 执行间隔（分钟）
// step2_duration: 二级时间步（秒），本地定时器定义及状态缓存有效期
// base_bucket_num: 每分钟初始扫描桶数
// bucket_num: 每分钟允许扩展到的最大桶数
// tasks_per_bucket: 每个桶承载的目标任务数，用于分钟级扩桶
// bucket_metadata_ttl: 时间片结束后 Redis 队列及分桶元数据额外保留秒数
// scan_interval: Scheduler 轮询间隔（秒）
// lock_expiration: 分布式锁初始 TTL（秒），必须大于时间片时长（60 秒）
// success_expiration: 分片扫描成功后的锁保留 TTL（秒），覆盖上一分钟回扫窗口
type SchedulerConfig struct {
	MigrateStepMinutes int `mapstructure:"migrate_step_minutes"`
	Step2Duration      int `mapstructure:"step2_duration"`
	BaseBucketNum      int `mapstructure:"base_bucket_num"`
	BucketNum          int `mapstructure:"bucket_num"`
	TasksPerBucket     int `mapstructure:"tasks_per_bucket"`
	BucketMetadataTTL  int `mapstructure:"bucket_metadata_ttl"`
	ScanInterval       int `mapstructure:"scan_interval"`
	LockExpiration     int `mapstructure:"lock_expiration"`
	SuccessExpiration  int `mapstructure:"success_expiration"`
}

// ExecutorConfig 执行器配置
type ExecutorConfig struct {
	WorkerPoolSize int `mapstructure:"worker_pool_size"` // 协程池大小
}

// MonitoringConfig controls periodic collection of health gauges.
type MonitoringConfig struct {
	CollectIntervalSeconds int    `mapstructure:"collect_interval_seconds"`
	PendingOverdueSeconds  int    `mapstructure:"pending_overdue_seconds"`
	RunningStaleSeconds    int    `mapstructure:"running_stale_seconds"`
	PrometheusURL          string `mapstructure:"prometheus_url"`
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

	// 读取环境变量
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
	normalizeMonitoringConfig(&cfg.Monitoring)

	AppConfig = cfg
	return cfg, nil
}

func normalizeSchedulerConfig(cfg *SchedulerConfig) {
	if cfg.BaseBucketNum < 1 {
		cfg.BaseBucketNum = 1
	}
	if cfg.BucketNum < cfg.BaseBucketNum {
		cfg.BucketNum = cfg.BaseBucketNum
	}
	if cfg.TasksPerBucket < 1 {
		cfg.TasksPerBucket = 100
	}
	if cfg.BucketMetadataTTL < 1 {
		cfg.BucketMetadataTTL = 600
	}
}

func normalizeMonitoringConfig(cfg *MonitoringConfig) {
	if cfg.CollectIntervalSeconds < 1 {
		cfg.CollectIntervalSeconds = 10
	}
	if cfg.PendingOverdueSeconds < 1 {
		cfg.PendingOverdueSeconds = 120
	}
	if cfg.RunningStaleSeconds < 1 {
		cfg.RunningStaleSeconds = 60
	}
	if cfg.PrometheusURL == "" {
		cfg.PrometheusURL = "http://localhost:9090"
	}
}

// setDefaults 设置配置默认值
func setDefaults() {
	// 服务器默认配置
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.mode", "debug")

	// 数据库默认配置
	viper.SetDefault("database.driver", "mysql")
	viper.SetDefault("database.max_open_conns", 100)
	viper.SetDefault("database.max_idle_conns", 10)
	viper.SetDefault("database.conn_max_lifetime", 60)

	// Redis 默认配置
	viper.SetDefault("redis.addr", "127.0.0.1:6379")
	viper.SetDefault("redis.password", "")
	viper.SetDefault("redis.db", 0)
	viper.SetDefault("redis.pool_size", 100)

	// 调度器默认配置
	viper.SetDefault("scheduler.migrate_step_minutes", 60) // 60 分钟
	viper.SetDefault("scheduler.step2_duration", 120)      // 2 分钟
	viper.SetDefault("scheduler.base_bucket_num", 1)
	viper.SetDefault("scheduler.bucket_num", 3)            // 动态分桶最大桶数
	viper.SetDefault("scheduler.tasks_per_bucket", 100)    // 每 100 个投递任务扩一个桶
	viper.SetDefault("scheduler.bucket_metadata_ttl", 600) // 时间片结束后保留 10 分钟
	viper.SetDefault("scheduler.scan_interval", 1)         // 1 秒
	viper.SetDefault("scheduler.lock_expiration", 70)      // 70 秒
	viper.SetDefault("scheduler.success_expiration", 130)  // 130 秒，分片成功扫描后的保留 TTL

	// 执行器默认配置
	viper.SetDefault("executor.worker_pool_size", 100)

	// 可观测性采集默认配置
	viper.SetDefault("monitoring.collect_interval_seconds", 10)
	viper.SetDefault("monitoring.pending_overdue_seconds", 120)
	viper.SetDefault("monitoring.running_stale_seconds", 60)
	viper.SetDefault("monitoring.prometheus_url", "http://localhost:9090")

	// 日志默认配置
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", "json")
	viper.SetDefault("log.output", "stdout")
}
