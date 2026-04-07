package handlers

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/harborworks/booking-hub/internal/api/middleware"
	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/service"
)

type GovernanceHandler struct {
	svc *service.GovernanceService
	log *slog.Logger
}

func NewGovernanceHandler(svc *service.GovernanceService, log *slog.Logger) *GovernanceHandler {
	return &GovernanceHandler{svc: svc, log: log}
}

// GET /api/governance/dictionary
func (h *GovernanceHandler) Dictionary(c *gin.Context) {
	out, err := h.svc.Dictionary(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"entries": out, "count": len(out)})
}

// GET /api/governance/tags
func (h *GovernanceHandler) Tags(c *gin.Context) {
	out, err := h.svc.Tags(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tags": out, "count": len(out)})
}

type consentRequest struct {
	Scope   string `json:"scope" binding:"required"`
	Version string `json:"version"`
}

// POST /api/consent/grant
func (h *GovernanceHandler) GrantConsent(c *gin.Context) {
	u := middleware.CurrentUser(c)
	var req consentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Version == "" {
		req.Version = "v1"
	}
	if err := h.svc.GrantConsent(c.Request.Context(), u.ID, req.Scope, req.Version); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "granted", "scope": req.Scope})
}

// POST /api/consent/withdraw
func (h *GovernanceHandler) WithdrawConsent(c *gin.Context) {
	u := middleware.CurrentUser(c)
	var req consentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Version == "" {
		req.Version = "v1"
	}
	if err := h.svc.WithdrawConsent(c.Request.Context(), u.ID, req.Scope, req.Version); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "withdrawn", "scope": req.Scope})
}

// GET /api/consent
func (h *GovernanceHandler) ListConsent(c *gin.Context) {
	u := middleware.CurrentUser(c)
	out, err := h.svc.ConsentHistory(c.Request.Context(), u.ID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"consent": out, "count": len(out)})
}

// POST /api/account/delete
func (h *GovernanceHandler) RequestDeletion(c *gin.Context) {
	u := middleware.CurrentUser(c)
	d, err := h.svc.RequestDeletion(c.Request.Context(), u.ID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"status":         "scheduled",
		"process_after": d.ProcessAfter,
		"detail":        "personal data will be hard-deleted on or after this timestamp; analytics will be anonymized but preserved",
	})
}

// POST /api/account/delete/cancel
func (h *GovernanceHandler) CancelDeletion(c *gin.Context) {
	u := middleware.CurrentUser(c)
	if err := h.svc.CancelDeletion(c.Request.Context(), u.ID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "canceled"})
}

// POST /api/admin/import/resources — multipart CSV upload, all-or-nothing.
func (h *GovernanceHandler) ImportResources(c *gin.Context) {
	u := middleware.CurrentUser(c)
	if u == nil || !u.IsAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected multipart 'file' field"})
		return
	}
	defer file.Close()
	count, errs, err := h.svc.ImportResourcesCSV(c.Request.Context(), file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(errs) > 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"status": "rejected",
			"errors": errs,
			"detail": "all-or-nothing: no rows imported",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "would_insert": count})
}

// GET /api/admin/export/resources.csv
func (h *GovernanceHandler) ExportResources(c *gin.Context) {
	u := middleware.CurrentUser(c)
	if u == nil || !u.IsAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return
	}
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", `attachment; filename="resources.csv"`)
	if err := h.svc.ExportResourcesCSV(c.Request.Context(), c.Writer); err != nil {
		writeServiceError(c, err)
	}
}

// keep domain referenced (for completeness)
var _ = domain.ErrInvalidInput
