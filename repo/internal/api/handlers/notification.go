package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/api/middleware"
	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/service"
	"github.com/harborworks/booking-hub/internal/views"
)

type NotificationHandler struct {
	svc *service.NotificationService
	log *slog.Logger
}

func NewNotificationHandler(svc *service.NotificationService, log *slog.Logger) *NotificationHandler {
	return &NotificationHandler{svc: svc, log: log}
}

// GET /api/notifications?unread=1
func (h *NotificationHandler) List(c *gin.Context) {
	u := middleware.CurrentUser(c)
	unread := c.Query("unread") == "1" || c.Query("unread") == "true"
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	out, err := h.svc.List(c.Request.Context(), u.ID, unread, limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"notifications": out, "count": len(out)})
}

// POST /api/notifications/:id/read
func (h *NotificationHandler) MarkRead(c *gin.Context) {
	u := middleware.CurrentUser(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.svc.MarkRead(c.Request.Context(), u.ID, id); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "read"})
}

// GET /api/notifications/unread-count
func (h *NotificationHandler) UnreadCount(c *gin.Context) {
	u := middleware.CurrentUser(c)
	n, err := h.svc.UnreadCount(c.Request.Context(), u.ID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"unread": n})
}

type createTodoRequest struct {
	TaskType string `json:"task_type" binding:"required"`
	Title    string `json:"title" binding:"required"`
	Payload  any    `json:"payload"`
	DueAt    string `json:"due_at"`
}

func (h *NotificationHandler) CreateTodo(c *gin.Context) {
	u := middleware.CurrentUser(c)
	var req createTodoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := h.svc.CreateTodo(c.Request.Context(), u.ID, req.TaskType, req.Title, req.Payload, nil)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, t)
}

// GET /api/todos?status=open
func (h *NotificationHandler) ListTodos(c *gin.Context) {
	u := middleware.CurrentUser(c)
	status := c.Query("status")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	out, err := h.svc.ListTodos(c.Request.Context(), u.ID, status, limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"todos": out, "count": len(out)})
}

type updateTodoRequest struct {
	Status string `json:"status" binding:"required"`
}

func (h *NotificationHandler) UpdateTodoStatus(c *gin.Context) {
	u := middleware.CurrentUser(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req updateTodoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.UpdateTodoStatus(c.Request.Context(), u.ID, id, domain.TodoStatus(req.Status)); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": req.Status})
}

// GET /api/admin/notification-deliveries — admin-only delivery audit log.
// Mounted under /api/admin which is gated by middleware.RequireAdmin().
func (h *NotificationHandler) AdminDeliveries(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	out, err := h.svc.AdminDeliveries(c.Request.Context(), limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deliveries": out, "count": len(out)})
}

// GET /notifications — HTML notification & to-do center.
func (h *NotificationHandler) CenterHTML(c *gin.Context) {
	user := middleware.CurrentUser(c)
	notifs, err := h.svc.List(c.Request.Context(), user.ID, false, 50)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	statusFilter := c.Query("status")
	todos, err := h.svc.ListTodos(c.Request.Context(), user.ID, statusFilter, 50)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	renderTempl(c, http.StatusOK, views.NotificationCenter(usernameOf(user), notifs, todos, statusFilter))
}
