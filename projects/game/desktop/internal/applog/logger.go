package applog

import (
	"sync"
	"sync/atomic"
	"time"
)

// Entry represents a single log entry.
type Entry struct {
	Time    string         `json:"time"`
	Level   string         `json:"level"`
	Source  string         `json:"source"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// Logger is an in-memory log store with optional event sink callback.
// It is safe for concurrent use.
type Logger struct {
	mu           sync.Mutex
	entries      []Entry
	eventSink    func(Entry)
	debugEnabled atomic.Bool
}

// NewLogger creates a new Logger.
func NewLogger() *Logger {
	return &Logger{}
}

// Info logs an info-level message. Source should be "backend" or "frontend".
func (l *Logger) Info(source, msg string, fields ...map[string]any) {
	l.log("info", source, msg, fields...)
}

// Error logs an error-level message. Source should be "backend" or "frontend".
func (l *Logger) Error(source, msg string, fields ...map[string]any) {
	l.log("error", source, msg, fields...)
}

// SetDebug enables or disables debug-level logging. When disabled, Debug is a
// zero-overhead no-op (no entry append, no event-sink push), so call sites may
// invoke Debug freely on hot paths. See specs/022-desktop-debug-mode/research.md D5.
func (l *Logger) SetDebug(enabled bool) {
	l.debugEnabled.Store(enabled)
}

// Debug logs a debug-level message. It is a no-op unless debug mode was enabled
// via SetDebug, so production (debug OFF) pays nothing. When enabled it flows
// the same Entry.Level/event-sink path as Info/Error.
// See specs/022-desktop-debug-mode/research.md D5.
func (l *Logger) Debug(source, msg string, fields ...map[string]any) {
	if !l.debugEnabled.Load() {
		return
	}
	l.log("debug", source, msg, fields...)
}

// Entries returns a copy of all log entries (prevents mutation of internal state).
func (l *Logger) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]Entry, len(l.entries))
	copy(result, l.entries)
	return result
}

// Clear removes all log entries.
func (l *Logger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = nil
}

// SetEventSink sets the callback for log event push.
// Pass nil to disable event push.
func (l *Logger) SetEventSink(fn func(Entry)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.eventSink = fn
}

// log is the internal logging method.
func (l *Logger) log(level, source, msg string, fields ...map[string]any) {
	entry := Entry{
		Time:    time.Now().UTC().Format(time.RFC3339),
		Level:   level,
		Source:  source,
		Message: msg,
	}
	if len(fields) > 0 {
		entry.Fields = fields[0]
	}
	l.mu.Lock()
	l.entries = append(l.entries, entry)
	sink := l.eventSink
	l.mu.Unlock()
	if sink != nil {
		sink(entry)
	}
}
