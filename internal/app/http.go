package app

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/handler"
	"github.com/chronoflow/internal/middleware"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/internal/service"
	"github.com/gin-gonic/gin"
)

type readinessChecker func(context.Context) map[string]string

type apiDependencies struct {
	timerService  *service.TimerService
	defRepo       repository.TimerDefinitionRepository
	executionRepo repository.ExecutionQueryRepository
}

func newHTTPHandler(
	cfg *config.Config,
	role Role,
	reporter *metrics.Reporter,
	checkReady readinessChecker,
	apiDeps *apiDependencies,
) (http.Handler, error) {
	gin.SetMode(cfg.Server.Mode)
	router := gin.New()
	router.Use(middleware.Logger())
	router.Use(middleware.CORS(cfg.Security.AllowedOrigins))
	router.Use(middleware.APISecurity(
		cfg.Security.APIKey,
		cfg.Security.MaxRequestBytes,
	))

	registerOperationalRoutes(
		router,
		role,
		role.Components(),
		reporter,
		checkReady,
	)
	if !role.RunsAPI() {
		router.NoRoute(func(c *gin.Context) {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    http.StatusNotFound,
				"message": "route is not available for this role",
				"role":    role,
			})
		})
		return router, nil
	}

	timerHandler := handler.NewTimerHandler(apiDeps.timerService)
	timerHandler.RegisterRoutes(router)
	executionHandler := handler.NewExecutionHandler(apiDeps.executionRepo)
	executionHandler.RegisterRoutes(router)
	monitoringHandler := handler.NewMonitoringHandler(
		apiDeps.defRepo,
		apiDeps.executionRepo,
		&cfg.Monitoring,
	)
	monitoringHandler.RegisterRoutes(router)

	if cfg.Monitoring.GrafanaURL != "" {
		grafanaTarget, err := url.Parse(cfg.Monitoring.GrafanaURL)
		if err != nil {
			return nil, err
		}
		grafanaProxy := httputil.NewSingleHostReverseProxy(grafanaTarget)
		router.Any("/grafana", gin.WrapH(grafanaProxy))
		router.Any("/grafana/*path", gin.WrapH(grafanaProxy))
	}

	router.Static("/assets", "./web/dist/assets")
	router.StaticFile("/", "./web/dist/index.html")
	router.NoRoute(func(c *gin.Context) {
		if c.Request.URL.Path == "/api" || strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    http.StatusNotFound,
				"message": "接口不存在",
			})
			return
		}
		c.File("./web/dist/index.html")
	})

	return router, nil
}

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
			"time":         time.Now().UTC().Format(time.RFC3339),
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
