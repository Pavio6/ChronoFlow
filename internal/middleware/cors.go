package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS 返回一个 Gin 中间件，用于处理跨域资源共享（CORS）请求
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 设置允许的来源，"*" 表示允许所有来源
		c.Header("Access-Control-Allow-Origin", "*")

		// 设置允许的 HTTP 方法
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")

		// 设置允许的请求头
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")

		// 设置预检请求的缓存时间（秒），减少 OPTIONS 请求次数
		c.Header("Access-Control-Max-Age", "86400")

		// 如果是 OPTIONS 预检请求，直接返回 204 No Content，不再继续处理
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		// 继续处理下一个中间件或路由处理器
		c.Next()
	}
}
