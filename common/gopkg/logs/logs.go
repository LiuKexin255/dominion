// Package logs provides a structured logging facade for dominion services.
package logs

import (
	"context"
	"log/slog"
	"os"
	"sync"

	"dominion/common/gopkg/otel"

	"go.opentelemetry.io/contrib/bridges/otelslog"
)

var (
	// newDeployHandler creates the handler for the deploy path.
	// Overridable for tests.
	newDeployHandler = func(name string) slog.Handler {
		return otelslog.NewHandler(name)
	}
)

var (
	defaultLogger *slog.Logger
	initOnce      = new(sync.Once)
)

func initLogger() {
	if otel.IsLoggerProviderSet() {
		defaultLogger = slog.New(newDeployHandler("dominion/common/gopkg/logs"))
	} else {
		handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
		defaultLogger = slog.New(handler)
	}
}

type loggerKey struct{}

// Default returns the lazily-initialized default logger.
// The first call auto-detects whether the process runs in a dominion
// deployment or local environment and creates an appropriate handler.
func Default() *slog.Logger {
	initOnce.Do(initLogger)
	return defaultLogger
}

// FromContext retrieves the logger stored in ctx.  If no logger is
// associated with ctx, it returns Default().
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return logger
	}
	return Default()
}

// InfoContext logs at LevelInfo using the logger associated with ctx.
func InfoContext(ctx context.Context, msg string, args ...any) {
	FromContext(ctx).InfoContext(ctx, msg, args...)
}

// ErrorContext logs at LevelError using the logger associated with ctx.
func ErrorContext(ctx context.Context, msg string, args ...any) {
	FromContext(ctx).ErrorContext(ctx, msg, args...)
}

// With attaches additional structured fields to the logger stored in ctx
// and returns a child context carrying the enriched logger.
func With(ctx context.Context, args ...any) context.Context {
	logger := FromContext(ctx).With(args...)
	return context.WithValue(ctx, loggerKey{}, logger)
}

// DebugContext logs at LevelDebug using the logger associated with ctx.
func DebugContext(ctx context.Context, msg string, args ...any) {
	FromContext(ctx).DebugContext(ctx, msg, args...)
}

// WarnContext logs at LevelWarn using the logger associated with ctx.
func WarnContext(ctx context.Context, msg string, args ...any) {
	FromContext(ctx).WarnContext(ctx, msg, args...)
}

// WithLogger stores the given logger in ctx and returns the new context.
// If logger is nil, ctx is returned unchanged so that FromContext falls
// back to the default logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerKey{}, logger)
}

// SetDefault replaces the package-level default logger.
// This is intended for testing; production code should rely on
// the auto-detected default.
func SetDefault(logger *slog.Logger) {
	initOnce.Do(func() {})
	defaultLogger = logger
}
