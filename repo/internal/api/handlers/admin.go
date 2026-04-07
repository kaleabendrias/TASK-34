package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/api/middleware"
	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/infrastructure/cache"
	"github.com/harborworks/booking-hub/internal/service"
)

// AdminHandler exposes operator-only endpoints: cache stats, webhooks,
// backups, manual job triggers.
type AdminHandler struct {
	cache    *cache.Cache
	webhooks *service.WebhookService
	backups  *service.BackupService
	log      *slog.Logger
}

func NewAdminHandler(cache *cache.Cache, webhooks *service.WebhookService, backups *service.BackupService, log *slog.Logger) *AdminHandler {
	return &AdminHandler{cache: cache, webhooks: webhooks, backups: backups, log: log}
}

func adminGate(c *gin.Context) bool {
	u := middleware.CurrentUser(c)
	if u == nil || !u.IsAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin only"})
		return false
	}
	return true
}

// GET /api/admin/cache/stats
func (h *AdminHandler) CacheStats(c *gin.Context) {
	if !adminGate(c) {
		return
	}
	n, ttl := h.cache.Stats()
	c.JSON(http.StatusOK, gin.H{
		"entries": n,
		"ttl":     ttl.String(),
		"bypass_header": middleware.CacheBypassHeader,
	})
}

// POST /api/admin/cache/purge
func (h *AdminHandler) CachePurge(c *gin.Context) {
	if !adminGate(c) {
		return
	}
	h.cache.Purge()
	c.JSON(http.StatusOK, gin.H{"status": "purged"})
}

// ---------- webhooks ----------

type webhookCreateRequest struct {
	Name         string            `json:"name" binding:"required"`
	TargetURL    string            `json:"target_url" binding:"required"`
	EventFilter  []string          `json:"event_filter"`
	FieldMapping map[string]string `json:"field_mapping"`
	Secret       string            `json:"secret"`
}

func (h *AdminHandler) WebhookCreate(c *gin.Context) {
	if !adminGate(c) {
		return
	}
	var req webhookCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	w, err := h.webhooks.Create(c.Request.Context(), &domain.Webhook{
		Name: req.Name, TargetURL: req.TargetURL, EventFilter: req.EventFilter,
		FieldMapping: req.FieldMapping, Secret: req.Secret, Enabled: true,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, w)
}

func (h *AdminHandler) WebhookList(c *gin.Context) {
	if !adminGate(c) {
		return
	}
	out, err := h.webhooks.List(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"webhooks": out, "count": len(out)})
}

func (h *AdminHandler) WebhookDisable(c *gin.Context) {
	if !adminGate(c) {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.webhooks.Disable(c.Request.Context(), id); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "disabled"})
}

func (h *AdminHandler) WebhookDeliveries(c *gin.Context) {
	if !adminGate(c) {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	out, err := h.webhooks.Deliveries(c.Request.Context(), limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deliveries": out, "count": len(out)})
}

// ---------- backups ----------

func (h *AdminHandler) BackupFull(c *gin.Context) {
	if !adminGate(c) {
		return
	}
	b, err := h.backups.TakeFull(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, b)
}

func (h *AdminHandler) BackupIncremental(c *gin.Context) {
	if !adminGate(c) {
		return
	}
	b, err := h.backups.TakeIncremental(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, b)
}

func (h *AdminHandler) BackupList(c *gin.Context) {
	if !adminGate(c) {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	out, err := h.backups.List(c.Request.Context(), limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"backups": out, "count": len(out)})
}

// GET /api/admin/backups/restore-plan — list of files needed for a full
// point-in-time restore (last full + every newer incremental). The actual
// apply step is intentionally omitted from the API: restoring a database is
// a manual, supervised operation in any environment.
func (h *AdminHandler) BackupRestorePlan(c *gin.Context) {
	if !adminGate(c) {
		return
	}
	plan, err := h.backups.PlanRestore(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusFailedDependency, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"steps":      plan,
		"sla_hours":  4,
		"detail":     "load each file in order; full first, then incrementals",
	})
}
