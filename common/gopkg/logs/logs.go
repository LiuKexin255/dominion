// Package logs provides a structured logging facade for dominion services.
package logs

import (
	"context"
	"log/slog"
	"os"
	"sync"

	"dominion/common/gopkg/logs/event"
)

var (
	// newConsoleHandler creates the handler for the console path.
	// Overridable for tests.
	newConsoleHandler = func() slog.Handler {
		return slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
)

var (
	defaultLogger *slog.Logger
	initOnce      = new(sync.Once)
)

var (
	reporterMu     sync.Mutex
	activeReporter *slog.Logger
)

type loggerKey struct{}

// Default returns the lazily-initialized default logger using the console
// handler. Once a reporter is installed via InstallReporter the package-level
// Info/Warn/Error/Debug functions route log records to that reporter instead;
// Default() continues to return the console logger for callers that bypass the
// package-level helpers.
func Default() *slog.Logger {
	initOnce.Do(func() {
		defaultLogger = slog.New(newConsoleHandler())
	})
	return defaultLogger
}

// FromContext retrieves the logger stored in ctx. If no logger is associated
// with ctx, it returns Default().
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return logger
	}
	return Default()
}

// With attaches the given events to the logger stored in ctx and returns a
// child context carrying the enriched logger.
func With(ctx context.Context, events ...event.Event) context.Context {
	var kvs []any
	for _, ev := range events {
		if ev.Key == "" && ev.Value == nil {
			continue
		}
		kvs = append(kvs, ev.Key, ev.Value)
	}
	var logger *slog.Logger
	if len(kvs) > 0 {
		logger = FromContext(ctx).With(kvs...)
	} else {
		logger = FromContext(ctx)
	}
	return context.WithValue(ctx, loggerKey{}, logger)
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
// Calling SetDefault bypasses the console handler — the injected logger is
// used directly. Intended for testing.
func SetDefault(logger *slog.Logger) {
	initOnce.Do(func() {})
	defaultLogger = logger
}

// Info logs at LevelInfo using the logger associated with ctx. If a reporter
// has been installed via InstallReporter the record is routed there instead of
// the context logger.
func Info(ctx context.Context, msg string, events ...event.Event) {
	logAttrs(ctx, slog.LevelInfo, msg, events...)
}

// Error logs at LevelError using the logger associated with ctx.
func Error(ctx context.Context, msg string, events ...event.Event) {
	logAttrs(ctx, slog.LevelError, msg, events...)
}

// Debug logs at LevelDebug using the logger associated with ctx.
func Debug(ctx context.Context, msg string, events ...event.Event) {
	logAttrs(ctx, slog.LevelDebug, msg, events...)
}

// Warn logs at LevelWarn using the logger associated with ctx.
func Warn(ctx context.Context, msg string, events ...event.Event) {
	logAttrs(ctx, slog.LevelWarn, msg, events...)
}

// logAttrs converts event.Event values to slog.Attr, skips zero-value events,
// and routes the log record to the active reporter (if installed) or the
// context logger.
func logAttrs(ctx context.Context, level slog.Level, msg string, events ...event.Event) {
	attrs := make([]slog.Attr, 0, len(events))
	for _, ev := range events {
		if ev.Key == "" && ev.Value == nil {
			continue
		}
		attrs = append(attrs, slog.Attr{Key: ev.Key, Value: slog.AnyValue(ev.Value)})
	}
	logger := FromContext(ctx)
	reporterMu.Lock()
	r := activeReporter
	reporterMu.Unlock()
	if r != nil {
		r.LogAttrs(ctx, level, msg, attrs...)
	} else {
		logger.LogAttrs(ctx, level, msg, attrs...)
	}
}

// InstallReporter installs logger as the active reporter. Package-level
// Info/Warn/Error/Debug calls route log records to the reporter while it is
// installed. Returns an uninstall function that restores the previous
// behaviour; the function only uninstalls the reporter it was created for,
// leaving any subsequently installed reporter in place.
// Panics if logger is nil.
func InstallReporter(logger *slog.Logger) func() {
	if logger == nil {
		panic("logs: InstallReporter called with nil logger")
	}
	reporterMu.Lock()
	activeReporter = logger
	reporterMu.Unlock()
	return func() {
		uninstallReporter(logger)
	}
}

// uninstallReporter removes logger from the active reporter slot only if it
// is still the current reporter.
func uninstallReporter(logger *slog.Logger) {
	reporterMu.Lock()
	defer reporterMu.Unlock()
	if activeReporter == logger {
		activeReporter = nil
	}
}
