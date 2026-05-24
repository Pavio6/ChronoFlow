package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config 应用全局配置
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Redis     RedisConfig     `mapstructure:"redis"`
	Scheduler SchedulerConfig `mapstructure:"scheduler"`
	Executor  ExecutorConfig  `mapstructure:"executor"`
	Retry     RetryConfig     `mapstructure:"retry"`
	Log       LogConfig       `mapstructure:"log"`
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
// step2_duration: 二级时间步（秒），内存缓存刷新间隔
// bucket_num: 分桶数量
// scan_interval: Scheduler 轮询间隔（秒）
type SchedulerConfig struct {
	MigrateStepMinutes int `mapstructure:"migrate_step_minutes"`
	Step2Duration      int `mapstructure:"step2_duration"`
	BucketNum          int `mapstructure:"bucket_num"`
	ScanInterval       int `mapstructure:"scan_interval"`
}

// ExecutorConfig 执行器配置
type ExecutorConfig struct {
	Timeout        int `mapstructure:"timeout"`          // HTTP 回调超时（秒）
	MaxRetries     int `mapstructure:"max_retries"`      // 最大重试次数
	WorkerPoolSize int `mapstructure:"worker_pool_size"` // 协程池大小
}

// RetryConfig 重试策略配置
type RetryConfig struct {
	Strategy        string  `mapstructure:"strategy"`         // exponential, fixed
	InitialInterval int     `mapstructure:"initial_interval"` // 初始间隔（秒）
	MaxInterval     int     `mapstructure:"max_interval"`     // 最大间隔（秒）
	Multiplier      float64 `mapstructure:"multiplier"`       // 乘数
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

	AppConfig = cfg
	return cfg, nil
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
	viper.SetDefault("scheduler.step2_duration", 300)       // 5 分钟
	viper.SetDefault("scheduler.bucket_num", 3)
	viper.SetDefault("scheduler.scan_interval", 1) // 1 秒

	// 执行器默认配置
	viper.SetDefault("executor.timeout", 30)
	viper.SetDefault("executor.max_retries", 3)
	viper.SetDefault("executor.worker_pool_size", 100)

	// 重试策略默认配置
	viper.SetDefault("retry.strategy", "exponential")
	viper.SetDefault("retry.initial_interval", 10)
	viper.SetDefault("retry.max_interval", 60)
	viper.SetDefault("retry.multiplier", 3.0)

	// 日志默认配置
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", "json")
	viper.SetDefault("log.output", "stdout")
}
