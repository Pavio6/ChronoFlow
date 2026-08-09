package app

import (
	"context"
	"net/http"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/middleware"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/gin-gonic/gin"
)

type readinessChecker func(context.Context) map[string]string

// newRouter 创建并配置通用 Gin 路由器。
func newRouter(cfg *config.Config) *gin.Engine {
	gin.SetMode(cfg.Server.Mode)
	router := gin.New()
	router.Use(middleware.Logger())
	router.Use(middleware.CORS(cfg.Security.AllowedOrigins))
	router.Use(middleware.APISecurity(cfg.Security.APIKey, cfg.Security.MaxRequestBytes))
	return router
}

// newOperationalHTTPHandler 创建仅暴露运行状态接口的 HTTP 处理器。
func newOperationalHTTPHandler(
	cfg *config.Config,
	role Role,
	reporter *metrics.Reporter,
	checkReady readinessChecker,
) http.Handler {
	router := newRouter(cfg)
	registerOperationalRoutes(router, role, role.Components(), reporter, checkReady)
	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    http.StatusNotFound,
			"message": "route is not available for this role",
			"role":    role,
		})
	})
	return router
}

// registerOperationalRoutes 注册健康检查、就绪检查和指标接口。
func registerOperationalRoutes(
	router *gin.Engine,
	role Role,
	components []string,
	reporter *metrics.Reporter,
	checkReady readinessChecker,
) {
	router.GET("/metrics", gin.WrapH(reporter.GetHandler()))

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":       "ok",
			"role":         role,
			"runtime_mode": role.RuntimeMode(),
			"components":   components,
			"time":         time.Now().Format(time.RFC3339),
		})
	})

	router.GET("/ready", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		failures := checkReady(ctx)
		if len(failures) > 0 {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":       "not_ready",
				"role":         role,
				"runtime_mode": role.RuntimeMode(),
				"components":   components,
				"dependencies": failures,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":       "ready",
			"role":         role,
			"runtime_mode": role.RuntimeMode(),
			"components":   components,
		})
	})
}
