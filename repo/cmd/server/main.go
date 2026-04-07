package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/harborworks/booking-hub/internal/api"
	"github.com/harborworks/booking-hub/internal/api/handlers"
	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/infrastructure/cache"
	"github.com/harborworks/booking-hub/internal/infrastructure/config"
	"github.com/harborworks/booking-hub/internal/infrastructure/crypto"
	"github.com/harborworks/booking-hub/internal/infrastructure/database"
	"github.com/harborworks/booking-hub/internal/infrastructure/jobs"
	applog "github.com/harborworks/booking-hub/internal/infrastructure/logger"
	"github.com/harborworks/booking-hub/internal/repository"
	"github.com/harborworks/booking-hub/internal/service"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "run container healthcheck against local /healthz and exit")
	flag.Parse()

	if *healthcheck {
		os.Exit(runHealthcheck())
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
		os.Exit(1)
	}

	logger := applog.New(cfg.LogLevel)
	logger.Info("starting harborworks booking hub",
		"env", cfg.AppEnv,
		"port", cfg.AppPort,
		"db_host", cfg.DBHost,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg, logger)
	if err != nil {
		logger.Error("database connect failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if cfg.RunMigrations {
		if err := database.Migrate(cfg, logger, "/app/migrations"); err != nil {
			logger.Error("migrations failed", "error", err)
			os.Exit(1)
		}
	}

	if cfg.RunSeed {
		if err := database.Seed(ctx, pool, logger, "/app/seed/seed.sql"); err != nil {
			logger.Error("seed failed", "error", err)
			os.Exit(1)
		}
	}

	// --- Locally managed encryption key ---
	keyMgr, err := crypto.LoadOrCreate("/app/keys/master.key")
	if err != nil {
		logger.Error("load encryption key failed", "error", err)
		os.Exit(1)
	}
	logger.Info("encryption key ready", "path", keyMgr.KeyPath())
	_ = keyMgr // available for booking notes encryption (used by services that need it)

	// --- Repositories ---
	userRepo := repository.NewUserRepository(pool)
	sessionRepo := repository.NewSessionRepository(pool)
	captchaRepo := repository.NewCaptchaRepository(pool)
	resourceRepo := repository.NewResourceRepository(pool)
	bookingRepo := repository.NewBookingRepository(pool)
	groupRepo := repository.NewGroupRepository(pool)
	groupBuyRepo := repository.NewGroupBuyRepository(pool)
	idemRepo := repository.NewIdempotencyRepository(pool)
	docRepo := repository.NewDocumentRepository(pool)
	notifRepo := repository.NewNotificationRepository(pool)
	analyticsRepo := repository.NewAnalyticsRepository(pool)
	govRepo := repository.NewGovernanceRepository(pool)
	webhookRepo := repository.NewWebhookRepository(pool)
	backupRepo := repository.NewBackupRepository(pool)

	// --- Services ---
	authSvc := service.NewAuthService(userRepo, sessionRepo, captchaRepo, logger, service.DefaultAuthSettings())
	resourceSvc := service.NewResourceService(resourceRepo, bookingRepo, logger)
	bookingSvc := service.NewBookingService(bookingRepo, resourceRepo, userRepo, logger, service.DefaultBookingPolicy())
	groupSvc := service.NewGroupService(groupRepo, bookingRepo, logger)
	notifSvc := service.NewNotificationService(notifRepo, logger)
	groupBuySvc := service.NewGroupBuyService(groupBuyRepo, resourceRepo, userRepo, notifRepo, logger)
	docSvc := service.NewDocumentService(docRepo, logger)
	analyticsSvc := service.NewAnalyticsService(analyticsRepo, "harborworks-anon-salt", logger)
	govSvc := service.NewGovernanceService(govRepo, userRepo, bookingRepo, resourceRepo, analyticsSvc, logger)
	webhookSvc := service.NewWebhookService(webhookRepo, logger)
	backupSvc := service.NewBackupService(pool, backupRepo, "/backups", logger)

	// --- Cache (60s TTL by spec) ---
	c := cache.New(cache.DefaultTTL)

	// --- Bootstrap admin user ---
	if cfg.RunSeed {
		if err := seedAdminUser(ctx, userRepo, logger); err != nil {
			logger.Warn("admin seed skipped", "error", err)
		}
	}

	// --- Background jobs ---
	runner := jobs.NewRunner(logger)
	runner.Add(jobs.Job{Name: "analytics-aggregate", Interval: 1 * time.Minute, Run: analyticsSvc.RunAggregation})
	runner.Add(jobs.Job{Name: "anomaly-detect", Interval: 5 * time.Minute, Run: analyticsSvc.RunAnomalyDetection})
	runner.Add(jobs.Job{Name: "groupbuy-sweep", Interval: 1 * time.Minute, Run: groupBuySvc.SweepExpired})
	runner.Add(jobs.Job{Name: "deletion-executor", Interval: 5 * time.Minute, Run: govSvc.RunDeletionExecutor})
	runner.Add(jobs.Job{Name: "webhook-deliver", Interval: 5 * time.Second, Run: webhookSvc.RunDeliveryCycle})
	runner.Add(jobs.Job{Name: "backup-incremental", Interval: 24 * time.Hour, Run: func(ctx context.Context) error {
		_, err := backupSvc.TakeIncremental(ctx)
		return err
	}})
	runner.Add(jobs.Job{Name: "backup-full-weekly", Interval: 7 * 24 * time.Hour, Run: func(ctx context.Context) error {
		_, err := backupSvc.TakeFull(ctx)
		return err
	}})
	runner.Start(ctx)
	defer runner.Stop()

	// --- Handlers ---
	authHandler := handlers.NewAuthHandler(authSvc, logger)
	resourceHandler := handlers.NewResourceHandler(resourceSvc, logger)
	bookingHandler := handlers.NewBookingHandler(bookingSvc, resourceSvc, logger)
	groupHandler := handlers.NewGroupHandler(groupSvc, logger)
	gbHandler := handlers.NewGroupBuyHandler(groupBuySvc, analyticsSvc, logger)
	docHandler := handlers.NewDocumentHandler(docSvc, analyticsSvc, logger)
	notifHandler := handlers.NewNotificationHandler(notifSvc, logger)
	analyticsHandler := handlers.NewAnalyticsHandler(analyticsSvc, logger)
	govHandler := handlers.NewGovernanceHandler(govSvc, logger)
	adminHandler := handlers.NewAdminHandler(c, webhookSvc, backupSvc, logger)
	healthHandler := handlers.NewHealthHandler(pool, logger)

	router := api.NewRouter(api.Deps{
		Logger:              logger,
		Auth:                authSvc,
		Cache:               c,
		Idempotency:         idemRepo,
		HealthHandler:       healthHandler,
		AuthHandler:         authHandler,
		ResourceHandler:     resourceHandler,
		BookingHandler:      bookingHandler,
		GroupHandler:        groupHandler,
		GroupBuyHandler:     gbHandler,
		DocumentHandler:     docHandler,
		NotificationHandler: notifHandler,
		AnalyticsHandler:    analyticsHandler,
		GovernanceHandler:   govHandler,
		AdminHandler:        adminHandler,
	})

	addr := net.JoinHostPort(cfg.AppHost, cfg.AppPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("http server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
	logger.Info("server stopped cleanly")
}

func seedAdminUser(ctx context.Context, repo repository.UserRepository, logger interface {
	Info(msg string, args ...any)
}) error {
	const (
		username = "harbormaster"
		password = "Harbor@Works2026!"
	)

	if existing, err := repo.GetByUsername(ctx, username); err == nil && existing != nil {
		// Make sure existing harbormaster has the admin flag.
		if !existing.IsAdmin {
			_ = repo.SetAdmin(ctx, existing.ID, true)
		}
		return nil
	} else if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u := &domain.User{
		Username:     username,
		PasswordHash: string(hash),
		IsAdmin:      true,
	}
	if err := repo.Create(ctx, u); err != nil {
		return err
	}
	logger.Info("default admin user seeded",
		"username", username,
		"password", password,
		"note", "change this credential before exposing the stack")
	return nil
}

// pool variable kept exported for any future helpers needing direct DB access.
var _ = (*pgxpool.Pool)(nil)

func runHealthcheck() int {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}
	url := "http://127.0.0.1:" + port + "/healthz"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck failed: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}
