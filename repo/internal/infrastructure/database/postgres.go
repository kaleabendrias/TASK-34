package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harborworks/booking-hub/internal/infrastructure/config"
)

// Connect waits for the database to be reachable and returns a pgxpool.Pool.
// It retries with backoff so the application is resilient to compose start order.
func Connect(ctx context.Context, cfg *config.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parse pool config: %w", err)
	}
	poolCfg.MaxConns = int32(cfg.DBMaxConns)
	poolCfg.MinConns = 1
	poolCfg.MaxConnLifetime = 30 * time.Minute

	const maxAttempts = 30
	var pool *pgxpool.Pool

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		pool, err = pgxpool.NewWithConfig(ctx, poolCfg)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err = pool.Ping(pingCtx)
			cancel()
			if err == nil {
				log.Info("database connection established",
					"attempt", attempt,
					"host", cfg.DBHost,
					"db", cfg.DBName,
				)
				return pool, nil
			}
			pool.Close()
		}

		log.Warn("database not ready, retrying",
			"attempt", attempt,
			"max", maxAttempts,
			"error", err.Error(),
		)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
		}
	}

	return nil, fmt.Errorf("database unreachable after %d attempts: %w", maxAttempts, err)
}

// Migrate applies all up migrations from the given filesystem directory.
func Migrate(cfg *config.Config, log *slog.Logger, migrationsDir string) error {
	if _, err := os.Stat(migrationsDir); err != nil {
		return fmt.Errorf("migrations directory %s: %w", migrationsDir, err)
	}

	source := "file://" + migrationsDir
	m, err := migrate.New(source, cfg.MigrateURL())
	if err != nil {
		return fmt.Errorf("migrate init: %w", err)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			log.Warn("migrate source close", "error", srcErr)
		}
		if dbErr != nil {
			log.Warn("migrate db close", "error", dbErr)
		}
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	version, dirty, verr := m.Version()
	if verr != nil && !errors.Is(verr, migrate.ErrNilVersion) {
		log.Warn("could not read migration version", "error", verr)
	} else {
		log.Info("migrations applied", "version", version, "dirty", dirty)
	}
	return nil
}

// Seed loads a SQL file and executes it inside a single transaction.
// Idempotency is the responsibility of the seed file (use ON CONFLICT etc).
func Seed(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read seed file: %w", err)
	}
	if len(data) == 0 {
		log.Info("seed file empty, skipping")
		return nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("seed begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, string(data)); err != nil {
		return fmt.Errorf("execute seed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("seed commit: %w", err)
	}
	log.Info("seed data applied", "path", path)
	return nil
}
