package app

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/chronoflow/internal/config"
	"github.com/chronoflow/internal/handler"
	"github.com/chronoflow/internal/pkg/metrics"
	"github.com/chronoflow/internal/repository"
	"github.com/chronoflow/internal/service"
	"github.com/gin-gonic/gin"
)

type apiDependencies struct {
	timerService  *service.TimerService
	defRepo       repository.TimerDefinitionRepository
	executionRepo repository.ExecutionQueryRepository
}

func newAPIHTTPHandler(
	cfg *config.Config,
	role Role,
	reporter *metrics.Reporter,
	checkReady readinessChecker,
	apiDeps *apiDependencies,
) (http.Handler, error) {
	router := newRouter(cfg)
	registerOperationalRoutes(router, role, role.Components(), reporter, checkReady)

	timerHandler := handler.NewTimerHandler(apiDeps.timerService)
	timerHandler.RegisterRoutes(router)
	executionHandler := handler.NewExecutionHandler(apiDeps.executionRepo)
	executionHandler.RegisterRoutes(router)
	monitoringHandler := handler.NewMonitoringHandler(apiDeps.defRepo, apiDeps.executionRepo, &cfg.Monitoring)
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
			c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "接口不存在"})
			return
		}
		c.File("./web/dist/index.html")
	})

	return router, nil
}
