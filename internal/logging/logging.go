// Package logging builds the process logger honoring LOG_LEVEL so operators
// can quiet noisy deployments without code changes.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New returns a structured logger writing to stderr at the given level
// (debug|info|warn|error; unknown values fall back to info).
func New(level string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv}))
}
