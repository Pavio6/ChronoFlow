package handler

import (
	"net/http"
	"strconv"

	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/service"
	"github.com/gin-gonic/gin"
)

// TimerHandler 定时器 HTTP API 处理器
// 提供定时器定义的 RESTful API
type TimerHandler struct {
	timerSvc *service.TimerService
}

// NewTimerHandler 创建定时器处理器实例
func NewTimerHandler(timerSvc *service.TimerService) *TimerHandler {
	return &TimerHandler{timerSvc: timerSvc}
}

// RegisterRoutes 注册路由
// 将所有定时器相关的 API 路由注册到 Gin 引擎
func (h *TimerHandler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/api/v1")
	{
		// 定时器定义创建后不可修改，仅允许查询、删除和状态变更
		api.POST("/timers", h.CreateTimer)
		api.GET("/timers", h.ListTimers)
		api.GET("/timers/:id", h.GetTimer)
		api.DELETE("/timers/:id", h.DeleteTimer)

		// 定时器状态管理
		api.POST("/timers/:id/activate", h.ActivateTimer)
		api.POST("/timers/:id/deactivate", h.DeactivateTimer)

	}
}

// CreateTimer 创建定时器
// POST /api/v1/timers
func (h *TimerHandler) CreateTimer(c *gin.Context) {
	var req model.CreateTimerDefinitionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": "invalid request parameters: " + err.Error(),
		})
		return
	}

	def, err := h.timerSvc.Create(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code":    http.StatusCreated,
		"message": "timer created",
		"data":    def,
	})
}

// GetTimer 获取定时器详情
// GET /api/v1/timers/:id
func (h *TimerHandler) GetTimer(c *gin.Context) {
	id, err := parseID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": "invalid ID",
		})
		return
	}

	def, err := h.timerSvc.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    http.StatusNotFound,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"data": def,
	})
}

// DeleteTimer 删除定时器（逻辑删除）
// DELETE /api/v1/timers/:id
func (h *TimerHandler) DeleteTimer(c *gin.Context) {
	id, err := parseID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": "invalid ID",
		})
		return
	}

	if err := h.timerSvc.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    http.StatusInternalServerError,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "timer deleted",
	})
}

// ListTimers 查询定时器列表
// GET /api/v1/timers
func (h *TimerHandler) ListTimers(c *gin.Context) {
	var req model.TimerDefinitionListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": "invalid request parameters: " + err.Error(),
		})
		return
	}

	resp, err := h.timerSvc.List(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    http.StatusInternalServerError,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"data": resp,
	})
}

// ActivateTimer 激活定时器
// POST /api/v1/timers/:id/activate
func (h *TimerHandler) ActivateTimer(c *gin.Context) {
	id, err := parseID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": "invalid ID",
		})
		return
	}

	if err := h.timerSvc.Activate(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "timer activated",
	})
}

// DeactivateTimer 停用定时器
// POST /api/v1/timers/:id/deactivate
func (h *TimerHandler) DeactivateTimer(c *gin.Context) {
	id, err := parseID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": "invalid ID",
		})
		return
	}

	if err := h.timerSvc.Deactivate(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "timer deactivated",
	})
}

// parseID 从 URL 参数中解析 ID
func parseID(c *gin.Context, param string) (int64, error) {
	return strconv.ParseInt(c.Param(param), 10, 64)
}
