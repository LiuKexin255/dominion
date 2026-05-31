//go:build windows

package capture

import (
	"testing"
)

func TestWindowRef_Fields(t *testing.T) {
	// given: a WindowRef value with known fields
	w := WindowRef{
		Handle:      42,
		Title:       "Test Window",
		ProcessID:   1234,
		WidthPx:     800,
		HeightPx:    600,
		ScaleFactor: 1.0,
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
	if w.WidthPx != 800 {
		t.Errorf("WidthPx: expected 800, got %d", w.WidthPx)
	}
	if w.HeightPx != 600 {
		t.Errorf("HeightPx: expected 600, got %d", w.HeightPx)
	}
	if w.ScaleFactor != 1.0 {
		t.Errorf("ScaleFactor: expected 1.0, got %f", w.ScaleFactor)
	}
}

func TestWindowBounds_Width(t *testing.T) {
	// given: table-driven test cases for Width calculation
	tests := []struct {
		name   string
		bounds WindowBounds
		want   int
	}{
		{name: "normal width", bounds: WindowBounds{Left: 0, Top: 0, Right: 100, Bottom: 50}, want: 100},
		{name: "offset origin", bounds: WindowBounds{Left: 10, Top: 20, Right: 210, Bottom: 70}, want: 200},
		{name: "zero width", bounds: WindowBounds{Left: 50, Top: 0, Right: 50, Bottom: 100}, want: 0},
		{name: "negative width", bounds: WindowBounds{Left: 200, Top: 0, Right: 100, Bottom: 100}, want: -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when: computing Width
			got := tt.bounds.Width()

			// then: result matches expected
			if got != tt.want {
				t.Errorf("Width() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWindowBounds_Height(t *testing.T) {
	// given: table-driven test cases for Height calculation
	tests := []struct {
		name   string
		bounds WindowBounds
		want   int
	}{
		{name: "normal height", bounds: WindowBounds{Left: 0, Top: 0, Right: 100, Bottom: 50}, want: 50},
		{name: "offset origin", bounds: WindowBounds{Left: 0, Top: 30, Right: 100, Bottom: 230}, want: 200},
		{name: "zero height", bounds: WindowBounds{Left: 0, Top: 50, Right: 100, Bottom: 50}, want: 0},
		{name: "negative height", bounds: WindowBounds{Left: 0, Top: 200, Right: 100, Bottom: 100}, want: -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when: computing Height
			got := tt.bounds.Height()

			// then: result matches expected
			if got != tt.want {
				t.Errorf("Height() = %d, want %d", got, tt.want)
			}
		})
	}
}
