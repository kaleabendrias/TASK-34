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

type GroupHandler struct {
	svc *service.GroupService
	log *slog.Logger
}

func NewGroupHandler(svc *service.GroupService, log *slog.Logger) *GroupHandler {
	return &GroupHandler{svc: svc, log: log}
}

type groupRequest struct {
	Name           string `json:"name" form:"name" binding:"required"`
	OrganizerName  string `json:"organizer_name" form:"organizer_name"`
	OrganizerEmail string `json:"organizer_email" form:"organizer_email" binding:"required,email"`
	Capacity       int    `json:"capacity" form:"capacity" binding:"required,min=1"`
	Notes          string `json:"notes" form:"notes"`
}

// canSeeOrganizerPII reports whether the requester is allowed to see the
// real organizer name and email on the supplied group. Owners and admins
// always can; everyone else (including anonymous browsers) gets the
// MaskedView() projection.
func canSeeOrganizerPII(user *domain.User, g *domain.GroupReservation) bool {
	if user == nil {
		return false
	}
	if user.IsAdmin {
		return true
	}
	if g.OrganizerID != nil && *g.OrganizerID == user.ID {
		return true
	}
	return false
}

// projectGroup masks PII unless the requester is an authorised viewer.
func projectGroup(user *domain.User, g domain.GroupReservation) domain.GroupReservation {
	if canSeeOrganizerPII(user, &g) {
		return g
	}
	return g.MaskedView()
}

// POST /api/groups
func (h *GroupHandler) Create(c *gin.Context) {
	var req groupRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g := &domain.GroupReservation{
		Name:           req.Name,
		OrganizerName:  req.OrganizerName,
		OrganizerEmail: req.OrganizerEmail,
		Capacity:       req.Capacity,
		Notes:          req.Notes,
	}
	// Stamp the authenticated user (if any) as the organizer-of-record so
	// the masking gate can later verify ownership without an email match.
	if user := middleware.CurrentUser(c); user != nil {
		id := user.ID
		g.OrganizerID = &id
	}
	created, err := h.svc.Create(c.Request.Context(), g)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	// Echo the freshly created group back unmodified — the user who just
	// created it is by definition the owner.
	c.JSON(http.StatusCreated, created)
}

// GET /api/groups (shared list — organiser PII is always masked here)
func (h *GroupHandler) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	groups, err := h.svc.List(c.Request.Context(), limit, offset)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	user := middleware.CurrentUser(c)
	out := make([]domain.GroupReservation, 0, len(groups))
	for _, g := range groups {
		out = append(out, projectGroup(user, g))
	}
	c.JSON(http.StatusOK, gin.H{"groups": out, "count": len(out)})
}

// GET /api/groups/:id
func (h *GroupHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	g, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	user := middleware.CurrentUser(c)
	c.JSON(http.StatusOK, projectGroup(user, *g))
}

// GET /groups (HTML)
func (h *GroupHandler) IndexHTML(c *gin.Context) {
	groups, err := h.svc.List(c.Request.Context(), 25, 0)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	user := middleware.CurrentUser(c)
	masked := make([]domain.GroupReservation, 0, len(groups))
	for _, g := range groups {
		masked = append(masked, projectGroup(user, g))
	}
	renderTempl(c, http.StatusOK, views.GroupIndex(usernameOf(user), masked))
}

// GET /groups/:id (HTML)
func (h *GroupHandler) DetailHTML(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.String(http.StatusBadRequest, "invalid id")
		return
	}
	g, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	user := middleware.CurrentUser(c)
	view := projectGroup(user, *g)
	renderTempl(c, http.StatusOK, views.GroupDetail(usernameOf(user), view))
}
