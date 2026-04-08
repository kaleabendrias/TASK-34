package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// ErrMissingAnalyticsSalt is returned by Load when the analytics
// anonymisation salt is not supplied via secure configuration. There is no
// development fallback literal — every environment (prod, staging, test,
// CI) must provide ANALYTICS_ANON_SALT explicitly or the server refuses
// to start.
var ErrMissingAnalyticsSalt = errors.New("ANALYTICS_ANON_SALT is required and has no default (set it via secure configuration, not via .env files)")

// Config holds all runtime configuration loaded from environment variables.
// Defaults are provided so the binary boots in any compose-managed container.
type Config struct {
	AppEnv   string
	AppHost  string
	AppPort  string
	LogLevel string

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string
	DBMaxConns int

	RunMigrations bool
	RunSeed       bool

	// CookieSecure controls the Secure flag on session cookies. Defaults
	// to true so production deployments are safe by default; tests/local
	// development can opt out via COOKIE_SECURE=false.
	CookieSecure bool

	// AnalyticsAnonSalt is the per-deployment secret used to derive the
	// hashed analytics user identifier. MUST be supplied via secure
	// configuration (ANALYTICS_ANON_SALT env var injected by the
	// orchestrator / secret store). There is NO development or unit-test
	// fallback literal — Load() returns ErrMissingAnalyticsSalt when the
	// value is absent, and every environment must provide it explicitly.
	AnalyticsAnonSalt string
}

func Load() (*Config, error) {
	c := &Config{
		AppEnv:     getenv("APP_ENV", "production"),
		AppHost:    getenv("APP_HOST", "0.0.0.0"),
		AppPort:    getenv("APP_PORT", "8080"),
		LogLevel:   getenv("LOG_LEVEL", "info"),
		DBHost:     getenv("DB_HOST", "db"),
		DBPort:     getenv("DB_PORT", "5432"),
		DBUser:     getenv("DB_USER", "harbor"),
		DBPassword: getenv("DB_PASSWORD", "harbor_secret"),
		DBName:     getenv("DB_NAME", "harborworks"),
		DBSSLMode:  getenv("DB_SSLMODE", "disable"),
	}

	maxConns, err := strconv.Atoi(getenv("DB_MAX_CONNS", "10"))
	if err != nil || maxConns <= 0 {
		return nil, fmt.Errorf("invalid DB_MAX_CONNS: %w", err)
	}
	c.DBMaxConns = maxConns

	c.RunMigrations = parseBool(getenv("RUN_MIGRATIONS", "true"))
	c.RunSeed = parseBool(getenv("RUN_SEED", "true"))
	c.CookieSecure = parseBool(getenv("COOKIE_SECURE", "true"))

	// Strict: no default, no development fallback. If ANALYTICS_ANON_SALT
	// is missing the server refuses to start so we can never accidentally
	// ship a build that hashes PII under a well-known literal.
	c.AnalyticsAnonSalt = strings.TrimSpace(os.Getenv("ANALYTICS_ANON_SALT"))
	if c.AnalyticsAnonSalt == "" {
		return nil, ErrMissingAnalyticsSalt
	}

	return c, nil
}

// DSN renders a libpq-style connection string.
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode,
	)
}

// MigrateURL returns a database URL suitable for golang-migrate.
func (c *Config) MigrateURL() string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DBUser, c.DBPassword),
		Host:   c.DBHost + ":" + c.DBPort,
		Path:   "/" + c.DBName,
	}
	q := u.Query()
	q.Set("sslmode", c.DBSSLMode)
	u.RawQuery = q.Encode()
	return u.String()
}

func getenv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
