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

func NewExecutionHandler(
	repo repository.ExecutionQueryRepository,
) *ExecutionHandler {
	return &ExecutionHandler{repo: repo}
}

func (h *ExecutionHandler) RegisterRoutes(router *gin.Engine) {
	api := router.Group("/api/v1")
	api.GET("/executions", h.List)
	api.GET("/executions/:id", h.Get)
	api.GET("/timers/:id/executions", h.GetByTimer)
}

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

func (h *ExecutionHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": http.StatusBadRequest, "message": "无效的 ID",
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
			"code": http.StatusNotFound, "message": "执行记录不存在",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": execution})
}

func (h *ExecutionHandler) GetByTimer(c *gin.Context) {
	timerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": http.StatusBadRequest, "message": "无效的 ID",
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
