package log

import (
	"strings"
	"sync"
	"testing"
)

// captureRecorder stores emitted events for test assertions.
type captureRecorder struct {
	mu     sync.Mutex
	events []struct {
		name string
		data interface{}
	}
}

func (r *captureRecorder) emit(name string, data interface{}) {
	r.mu.Lock()
	r.events = append(r.events, struct {
		name string
		data interface{}
	}{name: name, data: data})
	r.mu.Unlock()
}

func (r *captureRecorder) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func (r *captureRecorder) entryAt(i int) Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.events[i].data.(Entry)
}

func TestLogger_Levels(t *testing.T) {
	tests := []struct {
		name   string
		logFn  func(l *Logger)
		wantLv string
	}{
		{name: "info", logFn: func(l *Logger) { l.Info("mod", "msg", nil) }, wantLv: "info"},
		{name: "error", logFn: func(l *Logger) { l.Error("mod", "msg", nil) }, wantLv: "error"},
		{name: "warn", logFn: func(l *Logger) { l.Warn("mod", "msg", nil) }, wantLv: "warn"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := new(captureRecorder)
			l := NewLogger(rec.emit)
			tt.logFn(l)

			if got := rec.len(); got != 1 {
				t.Fatalf("events = %d, want 1", got)
			}
			entry := rec.entryAt(0)
			if entry.Level != tt.wantLv {
				t.Fatalf("Level = %q, want %q", entry.Level, tt.wantLv)
			}
			if entry.Module != "mod" {
				t.Fatalf("Module = %q, want %q", entry.Module, "mod")
			}
			if entry.Message != "msg" {
				t.Fatalf("Message = %q, want %q", entry.Message, "msg")
			}
			if entry.Timestamp == "" {
				t.Fatalf("Timestamp is empty, want RFC3339")
			}
		})
	}
}

func TestLogger_Printf(t *testing.T) {
	rec := new(captureRecorder)
	l := NewLogger(rec.emit)
	l.Printf("runtime", "handle control request: %v", "some error")

	if got := rec.len(); got != 1 {
		t.Fatalf("events = %d, want 1", got)
	}
	entry := rec.entryAt(0)
	if entry.Level != "info" {
		t.Fatalf("Level = %q, want info", entry.Level)
	}
	if !strings.Contains(entry.Message, "some error") {
		t.Fatalf("Message = %q, want to contain %q", entry.Message, "some error")
	}
}

func TestLogger_Errorf(t *testing.T) {
	rec := new(captureRecorder)
	l := NewLogger(rec.emit)
	l.Errorf("transport", "read error: %v", "conn reset")

	entry := rec.entryAt(0)
	if entry.Level != "error" {
		t.Fatalf("Level = %q, want error", entry.Level)
	}
	if !strings.Contains(entry.Message, "conn reset") {
		t.Fatalf("Message = %q, want to contain %q", entry.Message, "conn reset")
	}
}

func TestLogger_Fields(t *testing.T) {
	rec := new(captureRecorder)
	l := NewLogger(rec.emit)
	l.Info("mod", "msg", map[string]string{"key": "value"})

	entry := rec.entryAt(0)
	if entry.Fields["key"] != "value" {
		t.Fatalf("Fields[key] = %q, want %q", entry.Fields["key"], "value")
	}
}

func TestLogger_NilEmit(t *testing.T) {
	l := NewLogger(nil)
	// Should not panic.
	l.Info("mod", "msg", nil)
}

func TestLogger_NilLogger(t *testing.T) {
	var l *Logger
	// Should not panic.
	l.Info("mod", "msg", nil)
}

func TestSetGlobal(t *testing.T) {
	rec := new(captureRecorder)
	SetGlobal(NewLogger(rec.emit))
	defer SetGlobal(nil)

	Info("app", "started", nil)
	if got := rec.len(); got != 1 {
		t.Fatalf("events = %d, want 1", got)
	}

	Error("app", "failed", map[string]string{"code": "500"})
	if got := rec.len(); got != 2 {
		t.Fatalf("events = %d, want 2", got)
	}
}

func TestSetGlobal_Printf(t *testing.T) {
	rec := new(captureRecorder)
	SetGlobal(NewLogger(rec.emit))
	defer SetGlobal(nil)

	Printf("runtime", "count: %d", 42)
	if got := rec.len(); got != 1 {
		t.Fatalf("events = %d, want 1", got)
	}
	entry := rec.entryAt(0)
	if !strings.Contains(entry.Message, "42") {
		t.Fatalf("Message = %q, want to contain 42", entry.Message)
	}
}

func TestGlobal_NoLogger(t *testing.T) {
	SetGlobal(nil)
	// Should not panic when global logger is nil.
	Info("mod", "msg", nil)
	Error("mod", "msg", nil)
	Warn("mod", "msg", nil)
	Printf("mod", "msg")
	Errorf("mod", "msg")
	Warnf("mod", "msg")
}
