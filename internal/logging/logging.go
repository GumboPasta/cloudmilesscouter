// Package logging installs the process-wide slog logger for the Go services
// (Phase 6 Step 3). Every binary calls Setup once at startup so all log output —
// including lines emitted by the internal packages through the slog default —
// is structured and carries a "service" field identifying which binary wrote it.
package logging

import (
	"log/slog"
	"os"
)

// Setup makes a slog handler the process-wide default, tagged with the service
// name. format "json" (the default) is for aggregated/production logs; "text"
// swaps in the human-readable handler for local runs. An unrecognised level
// falls back to info.
func Setup(service, level, format string) {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var h slog.Handler
	switch format {
	case "text":
		h = slog.NewTextHandler(os.Stderr, opts)
	default:
		h = slog.NewJSONHandler(os.Stderr, opts)
	}

	slog.SetDefault(slog.New(h.WithAttrs([]slog.Attr{slog.String("service", service)})))
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
