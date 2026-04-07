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
)

type AnalyticsHandler struct {
	svc *service.AnalyticsService
	log *slog.Logger
}

func NewAnalyticsHandler(svc *service.AnalyticsService, log *slog.Logger) *AnalyticsHandler {
	return &AnalyticsHandler{svc: svc, log: log}
}

type trackRequest struct {
	EventType  string `json:"event_type" binding:"required"`
	TargetType string `json:"target_type" binding:"required"`
	TargetID   string `json:"target_id" binding:"required"`
}

// POST /api/analytics/track — public ingest endpoint (rate-limited at the
// reverse proxy in production). Cheap path: it just inserts a row.
func (h *AnalyticsHandler) Track(c *gin.Context) {
	var req trackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tid, err := uuid.Parse(req.TargetID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_id must be UUID"})
		return
	}
	var userID uuid.UUID
	if u := middleware.CurrentUser(c); u != nil {
		userID = u.ID
	}
	if err := h.svc.Track(c.Request.Context(), domain.AnalyticsEventType(req.EventType), req.TargetType, tid, userID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "tracked"})
}

// GET /api/analytics/top?days=7&limit=10
func (h *AnalyticsHandler) Top(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	out, err := h.svc.TopSessions(c.Request.Context(), days, limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"top": out, "count": len(out)})
}

// GET /api/analytics/trends?event_type=view — returns 7/30/90 day buckets.
func (h *AnalyticsHandler) Trends(c *gin.Context) {
	et := c.DefaultQuery("event_type", "view")
	out, err := h.svc.Trends(c.Request.Context(), domain.AnalyticsEventType(et))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"event_type": et,
		"day_7":      out[7],
		"day_30":     out[30],
		"day_90":     out[90],
	})
}

// GET /api/admin/anomalies
func (h *AnalyticsHandler) Anomalies(c *gin.Context) {
	u := middleware.CurrentUser(c)
	if u == nil || !u.IsAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	out, err := h.svc.Anomalies(c.Request.Context(), limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"anomalies": out, "count": len(out)})
}
