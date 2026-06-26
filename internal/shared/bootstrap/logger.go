// Package bootstrap provides shared startup primitives for stream-analytics
// binaries.  Each cmd/*/main.go delegates config loading, logger creation,
// shard resolution, and signal handling to this package so that bootstrap
// logic lives in exactly one place.
package bootstrap

import (
	"log/slog"
	"os"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/config"
)

// BuildLogger creates a structured *slog.Logger from the given LogConfig.
// If the level string is invalid, it defaults to info.
func BuildLogger(cfg config.LogConfig) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToLower(cfg.Level))); err != nil {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if strings.ToLower(cfg.Format) == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
