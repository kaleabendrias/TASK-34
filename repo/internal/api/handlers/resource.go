package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/api/middleware"
	"github.com/harborworks/booking-hub/internal/service"
	"github.com/harborworks/booking-hub/internal/views"
)

type ResourceHandler struct {
	svc *service.ResourceService
	log *slog.Logger
}

func NewResourceHandler(svc *service.ResourceService, log *slog.Logger) *ResourceHandler {
	return &ResourceHandler{svc: svc, log: log}
}

// GET /api/resources
func (h *ResourceHandler) List(c *gin.Context) {
	res, err := h.svc.List(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"resources": res, "count": len(res)})
}

// GET /api/availability?resource_id=<uuid>&date=YYYY-MM-DD
func (h *ResourceHandler) Availability(c *gin.Context) {
	rid, err := uuid.Parse(c.Query("resource_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resource_id must be a valid UUID"})
		return
	}
	dateStr := c.Query("date")
	if dateStr == "" {
		dateStr = time.Now().UTC().Format("2006-01-02")
	}
	day, err := time.ParseInLocation("2006-01-02", dateStr, time.UTC)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date must be YYYY-MM-DD"})
		return
	}
	result, err := h.svc.Availability(c.Request.Context(), rid, day)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GET /availability (HTML)
func (h *ResourceHandler) AvailabilityPage(c *gin.Context) {
	resources, err := h.svc.List(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	user := middleware.CurrentUser(c)

	var (
		result *service.AvailabilityResult
	)
	if rid := c.Query("resource_id"); rid != "" {
		parsed, err := uuid.Parse(rid)
		if err == nil {
			date := c.DefaultQuery("date", time.Now().UTC().Format("2006-01-02"))
			day, err := time.ParseInLocation("2006-01-02", date, time.UTC)
			if err == nil {
				result, _ = h.svc.Availability(c.Request.Context(), parsed, day)
			}
		}
	}
	renderTempl(c, http.StatusOK, views.AvailabilityPage(usernameOf(user), resources, result))
}
