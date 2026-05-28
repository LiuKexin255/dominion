package applog

import (
	"sync"
	"testing"
	"time"
)

// TestLogger_Info verifies that Info logs an entry with the correct level,
// source, message, fields, and a parseable timestamp.
func TestLogger_Info(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		msg     string
		fields  map[string]any
		wantLvl string
	}{
		{
			name:    "info with fields",
			source:  "backend",
			msg:     "test message",
			fields:  map[string]any{"key": "val"},
			wantLvl: "info",
		},
		{
			name:    "info without fields",
			source:  "frontend",
			msg:     "no fields message",
			fields:  nil,
			wantLvl: "info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: a new logger
			l := NewLogger()

			// when: logging an info message
			l.Info(tt.source, tt.msg, tt.fields)

			// then: verify the entry properties
			entries := l.Entries()
			if len(entries) != 1 {
				t.Fatalf("expected 1 entry, got %d", len(entries))
			}
			e := entries[0]
			if e.Level != tt.wantLvl {
				t.Errorf("expected level %q, got %q", tt.wantLvl, e.Level)
			}
			if e.Source != tt.source {
				t.Errorf("expected source %q, got %q", tt.source, e.Source)
			}
			if e.Message != tt.msg {
				t.Errorf("expected message %q, got %q", tt.msg, e.Message)
			}
			if e.Time == "" {
				t.Fatal("expected non-empty time")
			}
			if _, err := time.Parse(time.RFC3339, e.Time); err != nil {
				t.Errorf("expected parseable RFC3339 time, got %q: %v", e.Time, err)
			}
			if tt.fields != nil {
				if e.Fields == nil {
					t.Fatal("expected non-nil fields")
				}
				if e.Fields["key"] != "val" {
					t.Errorf("expected fields[key]='val', got %v", e.Fields["key"])
				}
			}
		})
	}
}

// TestLogger_Error verifies that Error logs an entry with level "error".
func TestLogger_Error(t *testing.T) {
	// given: a new logger
	l := NewLogger()

	// when: logging an error message
	l.Error("backend", "error message")

	// then: verify the entry has error level
	entries := l.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Level != "error" {
		t.Errorf("expected level 'error', got %q", entries[0].Level)
	}
	if entries[0].Source != "backend" {
		t.Errorf("expected source 'backend', got %q", entries[0].Source)
	}
	if entries[0].Message != "error message" {
		t.Errorf("expected message 'error message', got %q", entries[0].Message)
	}
}

// TestLogger_Entries_Order verifies that Entries returns entries in insertion order.
func TestLogger_Entries_Order(t *testing.T) {
	// given: a logger with multiple entries
	l := NewLogger()
	messages := []string{"first", "second", "third"}
	for _, m := range messages {
		l.Info("backend", m)
	}

	// when: retrieving entries
	entries := l.Entries()

	// then: entries are in insertion order and count matches
	if len(entries) != len(messages) {
		t.Fatalf("expected %d entries, got %d", len(messages), len(entries))
	}
	for i, e := range entries {
		if e.Message != messages[i] {
			t.Errorf("entry %d: expected message %q, got %q", i, messages[i], e.Message)
		}
	}
}

// TestLogger_Entries_Copy verifies that Entries returns a copy so mutations
// to the returned slice do not affect subsequent calls.
func TestLogger_Entries_Copy(t *testing.T) {
	// given: logger with one entry
	l := NewLogger()
	l.Info("backend", "msg1")

	// when: get entries and mutate the returned slice
	entries1 := l.Entries()
	entries1[0] = Entry{Level: "modified"}
	entries1 = append(entries1, Entry{})

	// then: a subsequent call returns the unmodified original
	entries2 := l.Entries()
	if len(entries2) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries2))
	}
	if entries2[0].Level != "info" {
		t.Errorf("copy was modified: level is %q", entries2[0].Level)
	}
	if entries2[0].Message != "msg1" {
		t.Errorf("copy was modified: message is %q", entries2[0].Message)
	}
}

// TestLogger_Clear verifies that Clear removes all entries.
func TestLogger_Clear(t *testing.T) {
	// given: logger with entries
	l := NewLogger()
	l.Info("backend", "msg1")
	l.Info("backend", "msg2")

	// when: clearing
	l.Clear()

	// then: entries is empty
	entries := l.Entries()
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(entries))
	}
}

// TestLogger_EventSink verifies the event sink callback mechanism:
// callback fires on log, can be replaced, and can be set to nil without panic.
func TestLogger_EventSink(t *testing.T) {
	t.Run("sink receives entries", func(t *testing.T) {
		// given: logger with event sink that captures entries
		l := NewLogger()
		var captured []Entry
		var mu sync.Mutex
		l.SetEventSink(func(e Entry) {
			mu.Lock()
			captured = append(captured, e)
			mu.Unlock()
		})

		// when: logging entries
		l.Info("backend", "msg1")
		l.Error("backend", "msg2")

		// then: sink was called for each entry
		mu.Lock()
		defer mu.Unlock()
		if len(captured) != 2 {
			t.Fatalf("expected 2 captured entries, got %d", len(captured))
		}
		if captured[0].Level != "info" {
			t.Errorf("expected first entry level 'info', got %q", captured[0].Level)
		}
		if captured[1].Level != "error" {
			t.Errorf("expected second entry level 'error', got %q", captured[1].Level)
		}
	})

	t.Run("replacing sink stops old callback", func(t *testing.T) {
		// given: logger with first sink
		l := NewLogger()
		var count1, count2 int
		var mu sync.Mutex
		l.SetEventSink(func(e Entry) {
			mu.Lock()
			count1++
			mu.Unlock()
		})

		// when: logging then replacing sink
		l.Info("backend", "before replace")

		l.SetEventSink(func(e Entry) {
			mu.Lock()
			count2++
			mu.Unlock()
		})
		l.Info("backend", "after replace")

		// then: old sink fired once, new sink fired once
		mu.Lock()
		defer mu.Unlock()
		if count1 != 1 {
			t.Errorf("expected old sink called 1 time, got %d", count1)
		}
		if count2 != 1 {
			t.Errorf("expected new sink called 1 time, got %d", count2)
		}
	})

	t.Run("nil sink does not panic", func(t *testing.T) {
		// given: logger with sink set to nil
		l := NewLogger()
		l.SetEventSink(func(e Entry) {})
		l.SetEventSink(nil)

		// when/then: logging does not panic
		l.Info("backend", "should not panic")
		l.Error("backend", "also should not panic")
	})
}

// TestLogger_Concurrent verifies that concurrent logging is safe and
// no entries are lost.
func TestLogger_Concurrent(t *testing.T) {
	// given: a logger and goroutine count
	l := NewLogger()
	n := 100
	var wg sync.WaitGroup

	// when: concurrent logging from multiple goroutines
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				l.Info("backend", "concurrent info")
			} else {
				l.Error("backend", "concurrent error")
			}
		}(i)
	}
	wg.Wait()

	// then: all entries were recorded with no races or lost entries
	entries := l.Entries()
	if len(entries) != n {
		t.Errorf("expected %d entries, got %d", n, len(entries))
	}
}
