package handler

import (
	"net/http"
	"strconv"

	"github.com/chronoflow/internal/model"
	"github.com/chronoflow/internal/service"
	"github.com/gin-gonic/gin"
)

// TaskHandler 任务 HTTP 处理器
type TaskHandler struct {
	taskService service.TaskService
	execService service.ExecutionService
}

// NewTaskHandler 创建任务处理器实例
func NewTaskHandler(taskService service.TaskService, execService service.ExecutionService) *TaskHandler {
	return &TaskHandler{
		taskService: taskService,
		execService: execService,
	}
}

// RegisterRoutes 注册路由
func (h *TaskHandler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/api/v1")
	{
		// 任务管理
		tasks := api.Group("/tasks")
		{
			tasks.POST("", h.CreateTask)
			tasks.GET("", h.ListTasks)
			tasks.GET("/:id", h.GetTask)
			tasks.PUT("/:id", h.UpdateTask)
			tasks.DELETE("/:id", h.DeleteTask)
			tasks.POST("/:id/enable", h.EnableTask)
			tasks.POST("/:id/disable", h.DisableTask)
			tasks.POST("/:id/trigger", h.TriggerTask)
		}

		// 执行记录
		executions := api.Group("/executions")
		{
			executions.GET("", h.ListExecutions)
			executions.GET("/:id", h.GetExecution)
		}
	}

	// 健康检查
	r.GET("/health", h.HealthCheck)
}

// CreateTask 创建任务
// @POST /api/v1/tasks
func (h *TaskHandler) CreateTask(c *gin.Context) {
	var req model.CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid request: " + err.Error(),
		})
		return
	}

	task, err := h.taskService.Create(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "failed to create task: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code":    201,
		"message": "task created",
		"data":    task,
	})
}

// GetTask 获取任务详情
// @GET /api/v1/tasks/:id
func (h *TaskHandler) GetTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid task id",
		})
		return
	}

	task, err := h.taskService.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "task not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": task,
	})
}

// UpdateTask 更新任务
// @PUT /api/v1/tasks/:id
func (h *TaskHandler) UpdateTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid task id",
		})
		return
	}

	var req model.UpdateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid request: " + err.Error(),
		})
		return
	}

	task, err := h.taskService.Update(id, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "failed to update task: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "task updated",
		"data":    task,
	})
}

// DeleteTask 删除任务
// @DELETE /api/v1/tasks/:id
func (h *TaskHandler) DeleteTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid task id",
		})
		return
	}

	if err := h.taskService.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "failed to delete task: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "task deleted",
	})
}

// ListTasks 查询任务列表
// @GET /api/v1/tasks
func (h *TaskHandler) ListTasks(c *gin.Context) {
	var req model.TaskListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid request: " + err.Error(),
		})
		return
	}

	result, err := h.taskService.List(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "failed to list tasks: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": result,
	})
}

// EnableTask 启用任务
// @POST /api/v1/tasks/:id/enable
func (h *TaskHandler) EnableTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid task id",
		})
		return
	}

	if err := h.taskService.Enable(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "failed to enable task: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "task enabled",
	})
}

// DisableTask 禁用任务
// @POST /api/v1/tasks/:id/disable
func (h *TaskHandler) DisableTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid task id",
		})
		return
	}

	if err := h.taskService.Disable(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "failed to disable task: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "task disabled",
	})
}

// TriggerTask 手动触发任务
// @POST /api/v1/tasks/:id/trigger
func (h *TaskHandler) TriggerTask(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid task id",
		})
		return
	}

	if err := h.taskService.Trigger(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "failed to trigger task: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "task triggered",
	})
}

// ListExecutions 查询执行记录列表
// @GET /api/v1/executions
func (h *TaskHandler) ListExecutions(c *gin.Context) {
	var req model.ExecutionListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid request: " + err.Error(),
		})
		return
	}

	result, err := h.execService.List(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "failed to list executions: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": result,
	})
}

// GetExecution 获取执行记录详情
// @GET /api/v1/executions/:id
func (h *TaskHandler) GetExecution(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "invalid execution id",
		})
		return
	}

	execution, err := h.execService.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "execution not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": execution,
	})
}

// HealthCheck 健康检查
// @GET /health
func (h *TaskHandler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"service": "ChronoFlow",
	})
}
