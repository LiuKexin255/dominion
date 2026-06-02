package operation_test

import (
	"testing"

	"dominion/projects/game/desktop/internal/operation"
)

func TestScreenshotToScreenCoords(t *testing.T) {
	tests := []struct {
		name        string
		screenshotX int32
		screenshotY int32
		windowLeft  int32
		windowTop   int32
		wantScreenX int32
		wantScreenY int32
	}{
		{
			name:        "window at (100,50) center click",
			screenshotX: 400,
			screenshotY: 300,
			windowLeft:  100,
			windowTop:   50,
			wantScreenX: 500,
			wantScreenY: 350,
		},
		{
			name:        "window at (0,0) click origin",
			screenshotX: 0,
			screenshotY: 0,
			windowLeft:  0,
			windowTop:   0,
			wantScreenX: 0,
			wantScreenY: 0,
		},
		{
			name:        "window at (200,150) right edge",
			screenshotX: 700,
			screenshotY: 500,
			windowLeft:  200,
			windowTop:   150,
			wantScreenX: 900,
			wantScreenY: 650,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sx, sy, err := operation.ScreenshotToScreenCoords(tt.screenshotX, tt.screenshotY, tt.windowLeft, tt.windowTop)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sx != tt.wantScreenX || sy != tt.wantScreenY {
				t.Errorf("got (%d,%d) want (%d,%d)", sx, sy, tt.wantScreenX, tt.wantScreenY)
			}
		})
	}
}

func TestValidateBounds(t *testing.T) {
	t.Run("in bounds", func(t *testing.T) {
		if err := operation.ValidateBounds(400, 300, 800, 600); err != nil {
			t.Errorf("unexpected error for in-bounds: %v", err)
		}
	})

	t.Run("out of bounds x too large", func(t *testing.T) {
		if err := operation.ValidateBounds(900, 300, 800, 600); err == nil {
			t.Error("expected error for out-of-bounds x")
		}
	})

	t.Run("out of bounds y too large", func(t *testing.T) {
		if err := operation.ValidateBounds(400, 700, 800, 600); err == nil {
			t.Error("expected error for out-of-bounds y")
		}
	})

	t.Run("negative x", func(t *testing.T) {
		if err := operation.ValidateBounds(-1, 100, 800, 600); err == nil {
			t.Error("expected error for negative x")
		}
	})

	t.Run("negative y", func(t *testing.T) {
		if err := operation.ValidateBounds(100, -1, 800, 600); err == nil {
			t.Error("expected error for negative y")
		}
	})

	t.Run("boundary x at width", func(t *testing.T) {
		if err := operation.ValidateBounds(800, 300, 800, 600); err == nil {
			t.Error("expected error for x == width")
		}
	})

	t.Run("boundary y at height", func(t *testing.T) {
		if err := operation.ValidateBounds(400, 600, 800, 600); err == nil {
			t.Error("expected error for y == height")
		}
	})
}

func TestScreenshotToScreenCoordsNegativeWindow(t *testing.T) {
	sx, sy, err := operation.ScreenshotToScreenCoords(100, 50, -50, -30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sx != 50 || sy != 20 {
		t.Errorf("got (%d,%d) want (50,20)", sx, sy)
	}
}
