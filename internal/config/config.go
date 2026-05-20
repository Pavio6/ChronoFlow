package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// Config 应用配置结构体
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Redis     RedisConfig     `mapstructure:"redis"`
	Scheduler SchedulerConfig `mapstructure:"scheduler"`
	Executor  ExecutorConfig  `mapstructure:"executor"`
	Retry     RetryConfig     `mapstructure:"retry"`
	Log       LogConfig       `mapstructure:"log"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"` // debug | release | test
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Driver          string `mapstructure:"driver"` // mysql | postgres
	DSN             string `mapstructure:"dsn"`
	MaxOpenConns    int    `mapstructure:"max_open_conns"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetime int    `mapstructure:"conn_max_lifetime"` // 秒
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	PoolSize int    `mapstructure:"pool_size"`
}

// SchedulerConfig 调度器配置
type SchedulerConfig struct {
	ScanInterval int `mapstructure:"scan_interval"` // 扫描间隔（秒）
	BatchSize    int `mapstructure:"batch_size"`    // 每批扫描任务数
}

// ExecutorConfig 执行器配置
type ExecutorConfig struct {
	Timeout        int `mapstructure:"timeout"`          // HTTP 回调超时时间（秒）
	MaxRetries     int `mapstructure:"max_retries"`      // 最大重试次数
	WorkerPoolSize int `mapstructure:"worker_pool_size"` // 工作池大小
}

// RetryConfig 重试策略配置
type RetryConfig struct {
	Strategy       string  `mapstructure:"strategy"`        // exponential | fixed
	InitialInterval int    `mapstructure:"initial_interval"` // 初始重试间隔（秒）
	MaxInterval     int    `mapstructure:"max_interval"`     // 最大重试间隔（秒）
	Multiplier      float64 `mapstructure:"multiplier"`      // 指数退避倍数
}

// LogConfig 日志配置
type LogConfig struct {
	Level    string `mapstructure:"level"`     // debug | info | warn | error
	Format   string `mapstructure:"format"`    // json | console
	Output   string `mapstructure:"output"`    // stdout | file
	FilePath string `mapstructure:"file_path"` // 日志文件路径
}

// AppConfig 全局配置实例
var AppConfig *Config

// Load 加载配置文件
// configPath: 配置文件路径（不包含扩展名）
func Load(configPath string) (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	if configPath != "" {
		viper.AddConfigPath(configPath)
	}
	viper.AddConfigPath("./config")
	viper.AddConfigPath(".")

	// 设置默认值
	setDefaults()

	// 读取环境变量
	viper.AutomaticEnv()

	// 读取配置文件
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	AppConfig = &config
	return &config, nil
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
	viper.SetDefault("database.conn_max_lifetime", 3600)

	// Redis 默认配置
	viper.SetDefault("redis.addr", "127.0.0.1:6379")
	viper.SetDefault("redis.db", 0)
	viper.SetDefault("redis.pool_size", 10)

	// 调度器默认配置
	viper.SetDefault("scheduler.scan_interval", 5)
	viper.SetDefault("scheduler.batch_size", 100)

	// 执行器默认配置
	viper.SetDefault("executor.timeout", 30)
	viper.SetDefault("executor.max_retries", 3)
	viper.SetDefault("executor.worker_pool_size", 10)

	// 重试默认配置
	viper.SetDefault("retry.strategy", "exponential")
	viper.SetDefault("retry.initial_interval", 10)
	viper.SetDefault("retry.max_interval", 60)
	viper.SetDefault("retry.multiplier", 3.0)

	// 日志默认配置
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", "json")
	viper.SetDefault("log.output", "stdout")
}

// GetServerAddr 获取服务器监听地址
func GetServerAddr() string {
	return fmt.Sprintf(":%d", AppConfig.Server.Port)
}

// GetDatabaseDSN 获取数据库连接字符串
func GetDatabaseDSN() string {
	return AppConfig.Database.DSN
}

// GetRedisAddr 获取 Redis 地址
func GetRedisAddr() string {
	return AppConfig.Redis.Addr
}

// GetScanInterval 获取扫描间隔
func GetScanInterval() time.Duration {
	return time.Duration(AppConfig.Scheduler.ScanInterval) * time.Second
}

// GetExecutorTimeout 获取执行器超时时间
func GetExecutorTimeout() time.Duration {
	return time.Duration(AppConfig.Executor.Timeout) * time.Second
}
