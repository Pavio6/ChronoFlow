package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 默认使用空日志器，避免测试或初始化失败路径发生 nil panic。
var log = zap.NewNop()

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
