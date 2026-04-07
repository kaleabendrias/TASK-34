package api

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/harborworks/booking-hub/internal/api/handlers"
	"github.com/harborworks/booking-hub/internal/api/middleware"
	"github.com/harborworks/booking-hub/internal/infrastructure/cache"
	"github.com/harborworks/booking-hub/internal/repository"
	"github.com/harborworks/booking-hub/internal/service"
)

// Deps groups every dependency the router needs. main.go is the only place
// that constructs this struct, keeping wiring explicit.
type Deps struct {
	Logger *slog.Logger
	Auth   *service.AuthService

	Cache       *cache.Cache
	Idempotency repository.IdempotencyRepository

	HealthHandler       *handlers.HealthHandler
	AuthHandler         *handlers.AuthHandler
	ResourceHandler     *handlers.ResourceHandler
	BookingHandler      *handlers.BookingHandler
	GroupHandler        *handlers.GroupHandler
	GroupBuyHandler     *handlers.GroupBuyHandler
	DocumentHandler     *handlers.DocumentHandler
	NotificationHandler *handlers.NotificationHandler
	AnalyticsHandler    *handlers.AnalyticsHandler
	GovernanceHandler   *handlers.GovernanceHandler
	AdminHandler        *handlers.AdminHandler
}

func NewRouter(d Deps) http.Handler {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(middleware.Recovery(d.Logger))
	r.Use(middleware.RequestLogger(d.Logger))

	soft := middleware.Authenticator(d.Auth, false)
	must := middleware.Authenticator(d.Auth, true)
	notBlacklisted := middleware.RequireNotBlacklisted()
	idem := middleware.Idempotency(d.Idempotency)
	cacheMW := middleware.ReadThroughCache(d.Cache)

	// Health
	r.GET("/healthz", d.HealthHandler.Liveness)
	r.GET("/readyz", d.HealthHandler.Readiness)

	// HTML pages
	r.GET("/", soft, d.BookingHandler.Index)
	r.GET("/auth/login", d.AuthHandler.LoginPage)
	r.GET("/auth/register", d.AuthHandler.RegisterPage)
	r.GET("/availability", soft, d.ResourceHandler.AvailabilityPage)
	r.GET("/bookings/new", must, notBlacklisted, d.BookingHandler.NewPage)
	r.GET("/groups", soft, d.GroupHandler.IndexHTML)
	r.GET("/groups/:id", soft, d.GroupHandler.DetailHTML)

	// JSON API
	api := r.Group("/api")
	{
		// --- auth (public) ---
		auth := api.Group("/auth")
		{
			auth.POST("/register", d.AuthHandler.Register)
			auth.POST("/login", d.AuthHandler.Login)
			auth.POST("/logout", soft, d.AuthHandler.Logout)
			auth.GET("/me", must, d.AuthHandler.Me)
			auth.GET("/captcha", d.AuthHandler.Captcha)
		}

		// --- read-only resource catalog (cached) ---
		api.GET("/resources", soft, cacheMW, d.ResourceHandler.List)
		api.GET("/availability", soft, cacheMW, d.ResourceHandler.Availability)

		// --- bookings ---
		bookings := api.Group("/bookings", must)
		{
			bookings.POST("", notBlacklisted, idem, d.BookingHandler.Create)
			bookings.GET("", d.BookingHandler.ListMine)
			bookings.GET("/:id", d.BookingHandler.Get)
			bookings.POST("/:id/transition", idem, d.BookingHandler.Transition)
		}

		// --- group reservations ---
		groups := api.Group("/groups")
		{
			groups.POST("", d.GroupHandler.Create)
			groups.GET("", cacheMW, d.GroupHandler.List)
			groups.GET("/:id", d.GroupHandler.Get)
		}

		// --- GROUP BUYS ---
		gb := api.Group("/group-buys")
		{
			gb.POST("", must, notBlacklisted, idem, d.GroupBuyHandler.Create)
			gb.GET("", soft, cacheMW, d.GroupBuyHandler.List)
			gb.GET("/:id", soft, d.GroupBuyHandler.Get)
			gb.GET("/:id/progress", soft, d.GroupBuyHandler.Progress)
			gb.GET("/:id/participants", soft, d.GroupBuyHandler.Participants)
			gb.POST("/:id/join", must, notBlacklisted, idem, d.GroupBuyHandler.Join)
		}

		// --- DOCUMENTS ---
		docs := api.Group("/documents", must)
		{
			docs.POST("/confirmation", idem, d.DocumentHandler.Confirmation)
			docs.POST("/checkin-pass", idem, d.DocumentHandler.CheckinPass)
			docs.GET("", d.DocumentHandler.List)
			docs.GET("/:id", d.DocumentHandler.Get)
			docs.GET("/:id/content", d.DocumentHandler.Content)
		}

		// --- NOTIFICATIONS & TODOS ---
		notify := api.Group("", must)
		{
			notify.GET("/notifications", d.NotificationHandler.List)
			notify.GET("/notifications/unread-count", d.NotificationHandler.UnreadCount)
			notify.POST("/notifications/:id/read", d.NotificationHandler.MarkRead)

			notify.POST("/todos", d.NotificationHandler.CreateTodo)
			notify.GET("/todos", d.NotificationHandler.ListTodos)
			notify.POST("/todos/:id/status", d.NotificationHandler.UpdateTodoStatus)
		}

		// --- ANALYTICS ---
		api.POST("/analytics/track", soft, d.AnalyticsHandler.Track)
		api.GET("/analytics/top", soft, cacheMW, d.AnalyticsHandler.Top)
		api.GET("/analytics/trends", soft, cacheMW, d.AnalyticsHandler.Trends)

		// --- GOVERNANCE ---
		api.GET("/governance/dictionary", soft, cacheMW, d.GovernanceHandler.Dictionary)
		api.GET("/governance/tags", soft, cacheMW, d.GovernanceHandler.Tags)
		api.POST("/consent/grant", must, d.GovernanceHandler.GrantConsent)
		api.POST("/consent/withdraw", must, d.GovernanceHandler.WithdrawConsent)
		api.GET("/consent", must, d.GovernanceHandler.ListConsent)
		api.POST("/account/delete", must, d.GovernanceHandler.RequestDeletion)
		api.POST("/account/delete/cancel", must, d.GovernanceHandler.CancelDeletion)

		// --- ADMIN ---
		admin := api.Group("/admin", must)
		{
			admin.GET("/notification-deliveries", d.NotificationHandler.AdminDeliveries)
			admin.GET("/anomalies", d.AnalyticsHandler.Anomalies)
			admin.POST("/import/resources", d.GovernanceHandler.ImportResources)
			admin.GET("/export/resources.csv", d.GovernanceHandler.ExportResources)
			admin.GET("/cache/stats", d.AdminHandler.CacheStats)
			admin.POST("/cache/purge", d.AdminHandler.CachePurge)
			admin.POST("/webhooks", d.AdminHandler.WebhookCreate)
			admin.GET("/webhooks", d.AdminHandler.WebhookList)
			admin.POST("/webhooks/:id/disable", d.AdminHandler.WebhookDisable)
			admin.GET("/webhooks/deliveries", d.AdminHandler.WebhookDeliveries)
			admin.POST("/backups/full", d.AdminHandler.BackupFull)
			admin.POST("/backups/incremental", d.AdminHandler.BackupIncremental)
			admin.GET("/backups", d.AdminHandler.BackupList)
			admin.GET("/backups/restore-plan", d.AdminHandler.BackupRestorePlan)
		}
	}

	return r
}
