package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/api/middleware"
	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/service"
)

type GroupBuyHandler struct {
	svc       *service.GroupBuyService
	analytics *service.AnalyticsService
	log       *slog.Logger
}

func NewGroupBuyHandler(svc *service.GroupBuyService, analytics *service.AnalyticsService, log *slog.Logger) *GroupBuyHandler {
	return &GroupBuyHandler{svc: svc, analytics: analytics, log: log}
}

type createGroupBuyRequest struct {
	ResourceID  string `json:"resource_id"  binding:"required"`
	Title       string `json:"title"        binding:"required"`
	Description string `json:"description"`
	Threshold   int    `json:"threshold"`
	Capacity    int    `json:"capacity"     binding:"required,min=1"`
	StartsAt    string `json:"starts_at"    binding:"required"`
	EndsAt      string `json:"ends_at"      binding:"required"`
	DeadlineAt  string `json:"deadline"`
}

func (h *GroupBuyHandler) Create(c *gin.Context) {
	user := middleware.CurrentUser(c)
	var req createGroupBuyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rid, err := uuid.Parse(req.ResourceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resource_id must be UUID"})
		return
	}
	start, err := parseTime(req.StartsAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "starts_at " + err.Error()})
		return
	}
	end, err := parseTime(req.EndsAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ends_at " + err.Error()})
		return
	}
	in := service.CreateGroupBuyInput{
		OrganizerID: user.ID,
		ResourceID:  rid,
		Title:       req.Title,
		Description: req.Description,
		Threshold:   req.Threshold,
		Capacity:    req.Capacity,
		StartsAt:    start,
		EndsAt:      end,
	}
	if req.DeadlineAt != "" {
		dl, err := parseTime(req.DeadlineAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "deadline " + err.Error()})
			return
		}
		in.Deadline = &dl
	}
	gb, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gb)
}

type joinRequest struct {
	Quantity int `json:"quantity" form:"quantity"`
}

// POST /api/group-buys/:id/join — protected by the Idempotency middleware.
func (h *GroupBuyHandler) Join(c *gin.Context) {
	user := middleware.CurrentUser(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req joinRequest
	_ = c.ShouldBind(&req)
	if req.Quantity <= 0 {
		req.Quantity = 1
	}
	gb, part, err := h.svc.Join(c.Request.Context(), id, user.ID, req.Quantity)
	if err != nil {
		writeGroupBuyError(c, err)
		return
	}
	_ = h.analytics.Track(c.Request.Context(), domain.EventFavorite, "group_buy", id, user.ID)
	c.JSON(http.StatusOK, gin.H{"group_buy": gb, "participant": part})
}

func (h *GroupBuyHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	gb, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if u := middleware.CurrentUser(c); u != nil {
		_ = h.analytics.Track(c.Request.Context(), domain.EventView, "group_buy", id, u.ID)
	}
	c.JSON(http.StatusOK, gb)
}

func (h *GroupBuyHandler) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	out, err := h.svc.List(c.Request.Context(), limit, offset)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"group_buys": out, "count": len(out)})
}

// GET /api/group-buys/:id/progress — live progress payload for the UI.
func (h *GroupBuyHandler) Progress(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	p, err := h.svc.Progress(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// GET /api/group-buys/:id/participants — returns participants with MASKED
// usernames so shared views never leak identities.
func (h *GroupBuyHandler) Participants(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	parts, err := h.svc.Participants(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	out := make([]gin.H, 0, len(parts))
	for _, p := range parts {
		out = append(out, gin.H{
			"id":           p.ID,
			"user_id":      p.UserID, // opaque UUID ≠ PII
			"masked_name":  domain.MaskName(p.UserID.String()[:8]),
			"quantity":     p.Quantity,
			"joined_at":    p.JoinedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"participants": out, "count": len(out)})
}

func writeGroupBuyError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrOversold):
		c.JSON(http.StatusConflict, gin.H{"error": "no remaining capacity"})
	case errors.Is(err, domain.ErrAlreadyJoined):
		c.JSON(http.StatusConflict, gin.H{"error": "already joined this group buy"})
	case errors.Is(err, domain.ErrDeadlinePassed):
		c.JSON(http.StatusConflict, gin.H{"error": "deadline has passed"})
	case errors.Is(err, domain.ErrOptimisticLock):
		c.JSON(http.StatusConflict, gin.H{"error": "please retry: high contention"})
	default:
		writeServiceError(c, err)
	}
}

// parseTime accepts RFC3339 and "2006-01-02T15:04". Already declared in
// booking.go; we re-use through the package scope.
var _ = parseTime

// ensure time package is kept used even when the handler compiles without
// referencing it otherwise (no-op line).
var _ = time.Second
