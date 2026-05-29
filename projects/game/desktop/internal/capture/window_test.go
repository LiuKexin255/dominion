//go:build windows

package capture

import (
	"testing"
)

func TestWindowRef_Fields(t *testing.T) {
	// given: a WindowRef value with known fields
	w := WindowRef{
		Handle:         42,
		Title:          "Test Window",
		ProcessID:      1234,
		ClientWidthPx:  800,
		ClientHeightPx: 600,
		ScaleFactor:    1.0,
	}

	// then: all fields accessible and equal
	if w.Handle != 42 {
		t.Errorf("Handle: expected 42, got %v", w.Handle)
	}
	if w.Title != "Test Window" {
		t.Errorf("Title: expected 'Test Window', got %q", w.Title)
	}
	if w.ProcessID != 1234 {
		t.Errorf("ProcessID: expected 1234, got %d", w.ProcessID)
	}
	if w.ClientWidthPx != 800 {
		t.Errorf("ClientWidthPx: expected 800, got %d", w.ClientWidthPx)
	}
	if w.ClientHeightPx != 600 {
		t.Errorf("ClientHeightPx: expected 600, got %d", w.ClientHeightPx)
	}
	if w.ScaleFactor != 1.0 {
		t.Errorf("ScaleFactor: expected 1.0, got %f", w.ScaleFactor)
	}
}
