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

type DocumentHandler struct {
	svc       *service.DocumentService
	analytics *service.AnalyticsService
	log       *slog.Logger
}

func NewDocumentHandler(svc *service.DocumentService, analytics *service.AnalyticsService, log *slog.Logger) *DocumentHandler {
	return &DocumentHandler{svc: svc, analytics: analytics, log: log}
}

type generateRequest struct {
	RelatedType string            `json:"related_type" binding:"required"`
	RelatedID   string            `json:"related_id" binding:"required"`
	Title       string            `json:"title" binding:"required"`
	Fields      map[string]string `json:"fields"`
}

// POST /api/documents/confirmation — generate (or supersede) a confirmation PDF.
func (h *DocumentHandler) Confirmation(c *gin.Context) {
	user := middleware.CurrentUser(c)
	var req generateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rid, err := uuid.Parse(req.RelatedID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "related_id must be UUID"})
		return
	}
	doc, rev, err := h.svc.GenerateConfirmation(c.Request.Context(), user.ID, req.RelatedType, rid, req.Title, req.Fields)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"document": doc, "revision": rev.Revision})
}

// POST /api/documents/checkin-pass — generate (or supersede) a check-in PNG.
func (h *DocumentHandler) CheckinPass(c *gin.Context) {
	user := middleware.CurrentUser(c)
	var req generateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rid, err := uuid.Parse(req.RelatedID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "related_id must be UUID"})
		return
	}
	doc, rev, err := h.svc.GenerateCheckinPass(c.Request.Context(), user.ID, req.RelatedType, rid, req.Title, req.Fields)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"document": doc, "revision": rev.Revision})
}

// GET /api/documents — list documents owned by the current user (includes
// supersession metadata per revision).
func (h *DocumentHandler) List(c *gin.Context) {
	user := middleware.CurrentUser(c)
	out, err := h.svc.ListByOwner(c.Request.Context(), user.ID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"documents": out, "count": len(out)})
}

// GET /api/documents/:id — metadata + revision history.
func (h *DocumentHandler) Get(c *gin.Context) {
	user := middleware.CurrentUser(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	d, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if d.OwnerUserID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	c.JSON(http.StatusOK, d)
}

// GET /api/documents/:id/content — secure byte retrieval (current revision).
// Optionally ?revision=N to pull a specific historic version (which will be
// labelled `superseded: true` in the JSON list — the bytes remain fetchable).
func (h *DocumentHandler) Content(c *gin.Context) {
	user := middleware.CurrentUser(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	meta, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if meta.OwnerUserID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}

	var rev *domain.DocumentRevision
	if r := c.Query("revision"); r != "" {
		n, err := strconv.Atoi(r)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "revision must be int"})
			return
		}
		rev, err = h.svc.GetRevision(c.Request.Context(), id, n)
		if err != nil {
			writeServiceError(c, err)
			return
		}
	} else {
		rev, err = h.svc.GetCurrent(c.Request.Context(), id)
		if err != nil {
			writeServiceError(c, err)
			return
		}
	}

	_ = h.analytics.Track(c.Request.Context(), domain.EventDownload, "document", id, user.ID)

	c.Header("Content-Type", rev.ContentType)
	c.Header("Content-Disposition", `inline; filename="`+meta.Title+`"`)
	c.Header("X-Revision", strconv.Itoa(rev.Revision))
	if rev.Superseded {
		c.Header("X-Superseded", "true")
	}
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write(rev.Content)
}
