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

	// newConsoleHandler creates the handler for the console path.
	// Overridable for tests.
	newConsoleHandler = func() slog.Handler {
		return slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	}

	// isInDeployMode reports whether the process is running in a dominion
	// deployment with a valid OTel log provider. It is overridable for tests.
	// In production it delegates to otel.IsLoggerProviderSet.
	isInDeployMode = otel.IsLoggerProviderSet
)

var (
	defaultLogger *slog.Logger
	initOnce      = new(sync.Once)
)

// dynamicHandler switches between console and OTel backends at log-write
// time based on whether the OTel LoggerProvider is available. Both handlers
// are pre-constructed; calls to Enabled and Handle are delegated to the
// currently-active handler.
type dynamicHandler struct {
	console slog.Handler
	otel    slog.Handler
}

// active returns the handler that should handle log records right now.
// The decision is made at call time, not construction time, so that a
// process can start logging before OTel is initialised and transparently
// switch to the OTel backend as soon as it becomes available.
func (h *dynamicHandler) active() slog.Handler {
	if isInDeployMode() {
		return h.otel
	}
	return h.console
}

// newDynamicHandler creates a dynamicHandler with both console and deploy
// handlers pre-constructed.
func newDynamicHandler() *dynamicHandler {
	return &dynamicHandler{
		console: newConsoleHandler(),
		otel:    newDeployHandler("dominion/common/gopkg/logs"),
	}
}

// Enabled reports whether the handler handles records at the given level.
// Delegates to the currently-active handler.
func (h *dynamicHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.active().Enabled(ctx, level)
}

// Handle processes the log record.
// Delegates to the currently-active handler.
func (h *dynamicHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.active().Handle(ctx, r)
}

// WithAttrs returns a new dynamicHandler with the given attributes applied
// to both the console and OTel handlers. This guarantees that attributes
// remain available regardless of which backend is active at write time.
func (h *dynamicHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &dynamicHandler{
		console: h.console.WithAttrs(attrs),
		otel:    h.otel.WithAttrs(attrs),
	}
}

// WithGroup returns a new dynamicHandler that opens a named group on both
// the console and OTel handlers.
func (h *dynamicHandler) WithGroup(name string) slog.Handler {
	return &dynamicHandler{
		console: h.console.WithGroup(name),
		otel:    h.otel.WithGroup(name),
	}
}

type loggerKey struct{}

// Default returns the lazily-initialized default logger.
// The underlying handler auto-switches between console and OTel backends
// based on the runtime deployment environment.
func Default() *slog.Logger {
	initOnce.Do(func() {
		defaultLogger = slog.New(newDynamicHandler())
	})
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
// This is intended for testing ONLY; production code should rely on
// the auto-switching mechanism provided by Default().
// Calling SetDefault bypasses the dynamic handler — the injected logger
// will not participate in console/OTel automatic switching.
func SetDefault(logger *slog.Logger) {
	initOnce.Do(func() {})
	defaultLogger = logger
}
