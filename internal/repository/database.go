package repository

import (
	"fmt"
	stdlog "log"
	"os"
	"strings"
	"time"

	"github.com/chronoflow/internal/config"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// DB 全局数据库连接实例
var DB *gorm.DB

// InitDatabase 初始化数据库连接
// 根据配置打开 MySQL 连接，并设置连接池参数
func InitDatabase(cfg *config.DatabaseConfig) error {
	var err error

	// 使用 GORM 打开 MySQL 连接，关闭默认事务以提升性能
	DB, err = gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{
		Logger: gormlogger.New(
			stdlog.New(os.Stdout, "\r\n", stdlog.LstdFlags),
			gormlogger.Config{
				SlowThreshold:             time.Second,
				LogLevel:                  databaseLogLevel(cfg.LogLevel),
				IgnoreRecordNotFoundError: true,
				ParameterizedQueries:      true,
				Colorful:                  false,
			},
		),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return fmt.Errorf("open database connection: %w", err)
	}

	// 获取底层的 *sql.DB 对象，用于配置连接池
	sqlDB, err := DB.DB()
	if err != nil {
		return fmt.Errorf("get database instance: %w", err)
	}

	// 设置最大打开连接数
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)

	// 设置最大空闲连接数
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)

	// 设置连接最大生命周期（配置值为分钟）
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Minute)

	return nil
}

// databaseLogLevel 将配置字符串转换为 GORM 日志级别
func databaseLogLevel(value string) gormlogger.LogLevel {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "silent":
		return gormlogger.Silent
	case "error":
		return gormlogger.Error
	case "info":
		return gormlogger.Info
	default:
		return gormlogger.Warn
	}
}

// CloseDatabase 关闭数据库连接
// 释放连接池中的所有连接资源
func CloseDatabase() {
	if DB == nil {
		return
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return
	}

	sqlDB.Close()
}
