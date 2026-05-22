package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/chronoflow/pkg/logger"
	"github.com/gin-gonic/gin"
)

// Recovery 返回一个 Gin 中间件，用于捕获 panic 并恢复服务
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 使用 defer 捕获 panic
		defer func() {
			if err := recover(); err != nil {
				// 获取 panic 时的堆栈信息
				stack := string(debug.Stack())

				// 使用 zap 记录错误和堆栈信息
				logger.Error("请求处理发生 panic",
					logger.Any("error", err),            // panic 的错误信息
					logger.String("stack", stack),        // 完整的堆栈跟踪
					logger.String("path", c.Request.URL.Path),   // 请求路径
					logger.String("method", c.Request.Method),   // 请求方法
				)

				// 返回 500 内部服务器错误的 JSON 响应
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"code":    http.StatusInternalServerError,
					"message": "服务器内部错误",
				})
			}
		}()

		// 继续处理请求
		c.Next()
	}
}
