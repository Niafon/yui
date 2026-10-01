// Package logging configures structured logging with trace propagation
// (NFR-005). Secrets never reach the logger: callers pass references, not
// values (SEC-006).
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type ctxKey string

const traceKey ctxKey = "trace_id"

// Setup installs the process logger and returns it.
func Setup(level string, jsonOutput bool) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	if jsonOutput {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	l := slog.New(h)
	slog.SetDefault(l)
	return l
}

// WithTrace stores a trace id in the context.
func WithTrace(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceKey, traceID)
}

// TraceID reads the trace id back, returning "" when absent.
func TraceID(ctx context.Context) string {
	v, _ := ctx.Value(traceKey).(string)
	return v
}

// From returns a logger already tagged with the context trace id.
func From(ctx context.Context) *slog.Logger {
	if id := TraceID(ctx); id != "" {
		return slog.Default().With("trace_id", id)
	}
	return slog.Default()
}
