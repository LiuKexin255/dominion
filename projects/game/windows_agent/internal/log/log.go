package log

import (
	"fmt"
	"sync"
	"time"
)

// Entry represents a structured log entry sent to the Wails frontend.
// The JSON field names must match what LogPanel.svelte expects.
type Entry struct {
	Timestamp string            `json:"timestamp"`
	Level     string            `json:"level"`
	Module    string            `json:"module"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields"`
}

// EventLogEntry is the Wails event name used to stream log entries to the frontend.
const EventLogEntry = "log:entry"

// EmitFunc emits a named event with data to the Wails frontend.
type EmitFunc func(name string, data interface{})

// Logger writes structured log entries to the Wails frontend log panel.
type Logger struct {
	emit EmitFunc
}

var (
	globalMu sync.RWMutex
	global   *Logger
)

// NewLogger creates a Logger that forwards entries via the provided EmitFunc.
func NewLogger(emit EmitFunc) *Logger {
	return &Logger{emit: emit}
}

// SetGlobal sets the package-level Logger used by the convenience functions.
func SetGlobal(l *Logger) {
	globalMu.Lock()
	global = l
	globalMu.Unlock()
}

func (l *Logger) emitEntry(level, module, message string, fields map[string]string) {
	if l == nil || l.emit == nil {
		return
	}
	l.emit(EventLogEntry, Entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		Module:    module,
		Message:   message,
		Fields:    fields,
	})
}

// Info logs an informational message from the given module.
func (l *Logger) Info(module, message string, fields map[string]string) {
	l.emitEntry("info", module, message, fields)
}

// Error logs an error message from the given module.
func (l *Logger) Error(module, message string, fields map[string]string) {
	l.emitEntry("error", module, message, fields)
}

// Warn logs a warning message from the given module.
func (l *Logger) Warn(module, message string, fields map[string]string) {
	l.emitEntry("warn", module, message, fields)
}

// Printf logs a formatted message at info level from the given module.
func (l *Logger) Printf(module, format string, args ...interface{}) {
	l.emitEntry("info", module, fmt.Sprintf(format, args...), nil)
}

// Errorf logs a formatted message at error level from the given module.
func (l *Logger) Errorf(module, format string, args ...interface{}) {
	l.emitEntry("error", module, fmt.Sprintf(format, args...), nil)
}

// Warnf logs a formatted message at warn level from the given module.
func (l *Logger) Warnf(module, format string, args ...interface{}) {
	l.emitEntry("warn", module, fmt.Sprintf(format, args...), nil)
}

// --- Package-level convenience functions ---

func getGlobal() *Logger {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return global
}

// Info logs at info level via the global logger.
func Info(module, message string, fields map[string]string) {
	if l := getGlobal(); l != nil {
		l.Info(module, message, fields)
	}
}

// Error logs at error level via the global logger.
func Error(module, message string, fields map[string]string) {
	if l := getGlobal(); l != nil {
		l.Error(module, message, fields)
	}
}

// Warn logs at warn level via the global logger.
func Warn(module, message string, fields map[string]string) {
	if l := getGlobal(); l != nil {
		l.Warn(module, message, fields)
	}
}

// Printf logs a formatted message at info level via the global logger.
func Printf(module, format string, args ...interface{}) {
	if l := getGlobal(); l != nil {
		l.Printf(module, format, args...)
	}
}

// Errorf logs a formatted message at error level via the global logger.
func Errorf(module, format string, args ...interface{}) {
	if l := getGlobal(); l != nil {
		l.Errorf(module, format, args...)
	}
}

// Warnf logs a formatted message at warn level via the global logger.
func Warnf(module, format string, args ...interface{}) {
	if l := getGlobal(); l != nil {
		l.Warnf(module, format, args...)
	}
}
