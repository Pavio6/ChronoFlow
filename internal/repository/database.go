package repository

import (
	"fmt"
	stdlog "log"
	"os"
	"strings"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/model"
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
		return fmt.Errorf("打开数据库连接失败: %w", err)
	}

	// 获取底层的 *sql.DB 对象，用于配置连接池
	sqlDB, err := DB.DB()
	if err != nil {
		return fmt.Errorf("获取数据库实例失败: %w", err)
	}

	// 设置最大打开连接数
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)

	// 设置最大空闲连接数
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)

	// 设置连接最大生命周期（配置值为分钟）
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Minute)

	return nil
}

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

// AutoMigrate 自动迁移数据库表结构
// 根据模型定义自动创建或更新表结构
func AutoMigrate() error {
	if DB == nil {
		return fmt.Errorf("数据库未初始化，请先调用 InitDatabase")
	}

	err := DB.AutoMigrate(
		&model.TimerDefinition{},
		&model.TimerExecution{},
		&model.OutboxEvent{},
	)
	if err != nil {
		return fmt.Errorf("自动迁移数据库表结构失败: %w", err)
	}

	return nil
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
