package handler

import (
	"net/http"
	"strconv"

	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/repository"
	"github.com/gin-gonic/gin"
)

type ExecutionHandler struct {
	repo repository.ExecutionQueryRepository
}

// NewExecutionHandler 创建 Execution 查询接口处理器。
func NewExecutionHandler(
	repo repository.ExecutionQueryRepository,
) *ExecutionHandler {
	return &ExecutionHandler{repo: repo}
}

// RegisterRoutes 注册 Execution 查询相关路由。
func (h *ExecutionHandler) RegisterRoutes(router *gin.Engine) {
	api := router.Group("/api/v1")
	api.GET("/executions", h.List)
	api.GET("/executions/:id", h.Get)
	api.GET("/timers/:id/executions", h.GetByTimer)
}

// List 分页返回符合筛选条件的 Execution 列表。
func (h *ExecutionHandler) List(c *gin.Context) {
	var request model.ExecutionListRequest
	if err := c.ShouldBindQuery(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": http.StatusBadRequest, "message": err.Error(),
		})
		return
	}
	if request.Page < 1 {
		request.Page = 1
	}
	if request.PageSize < 1 {
		request.PageSize = 20
	}
	items, total, stats, err := h.repo.ListExecutions(&request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": http.StatusInternalServerError, "message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"data": model.ExecutionListResponse{
			Total: total, Page: request.Page, PageSize: request.PageSize,
			Items: items, Stats: stats,
		},
	})
}

// Get 按 ID 返回单条 Execution。
func (h *ExecutionHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": http.StatusBadRequest, "message": "invalid ID",
		})
		return
	}
	execution, err := h.repo.GetExecutionByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": http.StatusInternalServerError, "message": err.Error(),
		})
		return
	}
	if execution == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code": http.StatusNotFound, "message": "execution does not exist",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": execution})
}

// GetByTimer 返回指定 Timer 最近的 Execution 列表。
func (h *ExecutionHandler) GetByTimer(c *gin.Context) {
	timerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": http.StatusBadRequest, "message": "invalid ID",
		})
		return
	}
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil &&
			parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	items, err := h.repo.GetExecutionsByTimerID(timerID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": http.StatusInternalServerError, "message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": items})
}
