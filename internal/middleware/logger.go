package middleware

import (
	"time"

	"github.com/chronoflow/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Logger 返回一个 Gin 中间件，用于记录每个请求的详细信息
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 记录请求开始时间
		start := time.Now()

		// 继续处理请求
		c.Next()

		// 计算请求处理耗时
		latency := time.Since(start)

		// 获取请求状态码
		statusCode := c.Writer.Status()

		// 使用 zap 日志记录请求详情
		logger.Info("HTTP request",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", statusCode),
			zap.Duration("latency", latency),
			zap.String("client_ip", c.ClientIP()),
		)
	}
}
