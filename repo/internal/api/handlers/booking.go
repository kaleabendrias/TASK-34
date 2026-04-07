package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/api/middleware"
	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/service"
	"github.com/harborworks/booking-hub/internal/views"
)

type BookingHandler struct {
	svc      *service.BookingService
	resource *service.ResourceService
	log      *slog.Logger
}

func NewBookingHandler(svc *service.BookingService, resource *service.ResourceService, log *slog.Logger) *BookingHandler {
	return &BookingHandler{svc: svc, resource: resource, log: log}
}

// ---------- requests ----------

type bookingRequest struct {
	ResourceID string `json:"resource_id" form:"resource_id" binding:"required"`
	GroupID    string `json:"group_id"    form:"group_id"`
	PartySize  int    `json:"party_size"  form:"party_size"`
	StartTime  string `json:"start_time"  form:"start_time" binding:"required"`
	EndTime    string `json:"end_time"    form:"end_time"   binding:"required"`
	Notes      string `json:"notes"       form:"notes"`
}

type transitionRequest struct {
	TargetState string `json:"target_state" form:"target_state" binding:"required"`
}

func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	// HTML <input type="datetime-local"> uses "2006-01-02T15:04"
	if t, err := time.Parse("2006-01-02T15:04", s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, errors.New("must be RFC3339 or YYYY-MM-DDTHH:MM")
}

// ---------- JSON endpoints ----------

// POST /api/bookings
func (h *BookingHandler) Create(c *gin.Context) {
	user := middleware.CurrentUser(c)

	var req bookingRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rid, err := uuid.Parse(req.ResourceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resource_id must be a valid UUID"})
		return
	}
	start, err := parseTime(req.StartTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start_time " + err.Error()})
		return
	}
	end, err := parseTime(req.EndTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end_time " + err.Error()})
		return
	}
	in := service.CreateInput{
		UserID:     user.ID,
		ResourceID: rid,
		PartySize:  req.PartySize,
		StartTime:  start,
		EndTime:    end,
		Notes:      req.Notes,
	}
	if req.GroupID != "" {
		gid, err := uuid.Parse(req.GroupID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "group_id must be a valid UUID"})
			return
		}
		in.GroupID = &gid
	}

	b, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, b)
}

// GET /api/bookings  (mine)
func (h *BookingHandler) ListMine(c *gin.Context) {
	user := middleware.CurrentUser(c)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	bs, err := h.svc.ListByUser(c.Request.Context(), user.ID, limit, offset)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"bookings": bs, "count": len(bs)})
}

// GET /api/bookings/:id
func (h *BookingHandler) Get(c *gin.Context) {
	user := middleware.CurrentUser(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	b, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if b.UserID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	c.JSON(http.StatusOK, b)
}

// POST /api/bookings/:id/transition
func (h *BookingHandler) Transition(c *gin.Context) {
	user := middleware.CurrentUser(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req transitionRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	target := domain.BookingStatus(req.TargetState)
	b, err := h.svc.Transition(c.Request.Context(), user.ID, id, target)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, b)
}

// ---------- HTML pages ----------

// GET / (dashboard / my bookings)
func (h *BookingHandler) Index(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		renderTempl(c, http.StatusOK, views.LandingPage())
		return
	}
	bs, err := h.svc.ListByUser(c.Request.Context(), user.ID, 50, 0)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	renderTempl(c, http.StatusOK, views.MyBookingsPage(user.Username, user.IsBlacklisted, user.BlacklistReason, bs))
}

// GET /bookings/new
func (h *BookingHandler) NewPage(c *gin.Context) {
	user := middleware.CurrentUser(c)
	resources, err := h.resource.List(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	renderTempl(c, http.StatusOK, views.NewBookingPage(user.Username, resources, c.Query("error")))
}

// ---------- helpers ----------

func writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, domain.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, domain.ErrLeadTime),
		errors.Is(err, domain.ErrCutoff),
		errors.Is(err, domain.ErrDailyLimit),
		errors.Is(err, domain.ErrOverlap),
		errors.Is(err, domain.ErrInvalidTransition),
		errors.Is(err, domain.ErrCapacityExceed),
		errors.Is(err, domain.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, domain.ErrBlacklisted), errors.Is(err, domain.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

func renderTempl(c *gin.Context, status int, comp templ.Component) {
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := comp.Render(c.Request.Context(), c.Writer); err != nil {
		// Best effort: log via standard logger if Render fails mid-stream.
		_ = c.Error(err)
	}
}

func usernameOf(u *domain.User) string {
	if u == nil {
		return ""
	}
	return u.Username
}
