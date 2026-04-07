package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New constructs a structured slog.Logger emitting JSON to stdout.
func New(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     lvl,
		AddSource: false,
	})
	return slog.New(handler).With(
		"service", "harborworks-booking-hub",
	)
}
