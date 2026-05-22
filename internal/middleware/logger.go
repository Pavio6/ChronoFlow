package middleware

import (
	"time"

	"github.com/chronoflow/pkg/logger"
	"github.com/gin-gonic/gin"
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
		logger.Info("HTTP 请求",
			logger.String("method", c.Request.Method),        // 请求方法（GET、POST 等）
			logger.String("path", c.Request.URL.Path),        // 请求路径
			logger.Int("status", statusCode),                 // 响应状态码
			logger.Duration("latency", latency),              // 请求处理耗时
			logger.String("client_ip", c.ClientIP()),         // 客户端 IP 地址
		)
	}
}
