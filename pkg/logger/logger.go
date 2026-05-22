package logger

import (
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 全局日志实例
var log *zap.Logger

// Init 初始化日志
// level: debug, info, warn, error
// format: json, console
// output: stdout, file
func Init(level, format, output, filePath string) {
	// 解析日志级别
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	// 配置编码器
	encoderConfig := zap.NewProductionConfig().EncoderConfig
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

	// 选择编码格式
	var encoder zapcore.Encoder
	if format == "console" {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	// 选择输出目标
	var writeSyncer zapcore.WriteSyncer
	if output == "file" && filePath != "" {
		file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			writeSyncer = zapcore.AddSync(os.Stdout)
		} else {
			writeSyncer = zapcore.AddSync(file)
		}
	} else {
		writeSyncer = zapcore.AddSync(os.Stdout)
	}

	// 创建核心
	core := zapcore.NewCore(encoder, writeSyncer, zapLevel)
	log = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(0))
}

// Sync 刷新日志缓冲区
func Sync() {
	if log != nil {
		log.Sync()
	}
}

// Info 信息日志
func Info(msg string, fields ...zap.Field) {
	log.Info(msg, fields...)
}

// Error 错误日志
func Error(msg string, fields ...zap.Field) {
	log.Error(msg, fields...)
}

// Warn 警告日志
func Warn(msg string, fields ...zap.Field) {
	log.Warn(msg, fields...)
}

// Debug 调试日志
func Debug(msg string, fields ...zap.Field) {
	log.Debug(msg, fields...)
}

// Fatal 致命错误日志
func Fatal(msg string, fields ...zap.Field) {
	log.Fatal(msg, fields...)
}

// String 构建字符串类型的日志字段
func String(key, val string) zap.Field {
	return zap.String(key, val)
}

// Int 构建整数类型的日志字段
func Int(key string, val int) zap.Field {
	return zap.Int(key, val)
}

// Int64 构建 64 位整数类型的日志字段
func Int64(key string, val int64) zap.Field {
	return zap.Int64(key, val)
}

// Float64 构建浮点数类型的日志字段
func Float64(key string, val float64) zap.Field {
	return zap.Float64(key, val)
}

// Duration 构建时间间隔类型的日志字段
func Duration(key string, val time.Duration) zap.Field {
	return zap.Duration(key, val)
}

// Any 构建任意类型的日志字段
func Any(key string, val interface{}) zap.Field {
	return zap.Any(key, val)
}

// Bool 构建布尔类型的日志字段
func Bool(key string, val bool) zap.Field {
	return zap.Bool(key, val)
}
