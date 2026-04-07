package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HealthHandler struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

func NewHealthHandler(pool *pgxpool.Pool, log *slog.Logger) *HealthHandler {
	return &HealthHandler{pool: pool, log: log}
}

// Liveness reports whether the process is up. Used by container orchestrators.
func (h *HealthHandler) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "alive"})
}

// Readiness verifies dependencies (database) are reachable.
func (h *HealthHandler) Readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := h.pool.Ping(ctx); err != nil {
		h.log.Warn("readiness failed", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "degraded",
			"db":     "unreachable",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready", "db": "ok"})
}
