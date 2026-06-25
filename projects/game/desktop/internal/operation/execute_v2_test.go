package operation

import (
	"testing"

	"dominion/projects/game"
)

func Test_normalizeAbsoluteVirtual(t *testing.T) {
	tests := []struct {
		name   string
		pixel  int32
		origin int32
		dim    int32
		want   int32
	}{
		{name: "primary origin maps to 0", pixel: 0, origin: 0, dim: 1920, want: 0},
		{name: "last pixel maps to 65535 at 1920", pixel: 1919, origin: 0, dim: 1920, want: 65535},
		{name: "last pixel maps to 65535 at 1080", pixel: 1079, origin: 0, dim: 1080, want: 65535},
		{name: "center of 1920", pixel: 960, origin: 0, dim: 1920, want: (960 * 65535) / 1919},
		{name: "100px on 800-wide screen", pixel: 100, origin: 0, dim: 800, want: (100 * 65535) / 799},
		{name: "degenerate dim 1 returns 0", pixel: 500, origin: 0, dim: 1, want: 0},
		{name: "degenerate dim 0 returns 0", pixel: 500, origin: 0, dim: 0, want: 0},
		{name: "negative dim returns 0", pixel: 500, origin: 0, dim: -10, want: 0},
		{name: "negative-origin virtual left edge maps to 0", pixel: -1920, origin: -1920, dim: 3840, want: 0},
		{name: "primary origin (0) maps mid-range on virtual desk", pixel: 0, origin: -1920, dim: 3840, want: (1920 * 65535) / 3839},
		{name: "virtual right edge maps to 65535", pixel: 1919, origin: -1920, dim: 3840, want: 65535},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeAbsoluteVirtual(tt.pixel, tt.origin, tt.dim)
			if got != tt.want {
				t.Errorf("normalizeAbsoluteVirtual(%d, %d, %d) = %d, want %d",
					tt.pixel, tt.origin, tt.dim, got, tt.want)
			}
		})
	}
}

func Test_normalizeAbsoluteVirtual_AlwaysInRange(t *testing.T) {
	for dim := int32(3); dim <= 3840; dim += 37 {
		originStep := dim / 3
		if originStep < 1 {
			originStep = 1
		}
		for origin := -dim; origin <= dim; origin += originStep {
			for pixel := origin; pixel < origin+dim; pixel += 41 {
				got := normalizeAbsoluteVirtual(pixel, origin, dim)
				if got < 0 || got > mouseAbsoluteMax {
					t.Errorf("normalizeAbsoluteVirtual(%d, %d, %d) = %d, outside [0, %d]",
						pixel, origin, dim, got, mouseAbsoluteMax)
				}
			}
		}
	}
}

func Test_validateScreenCoords(t *testing.T) {
	t.Run("in bounds on primary", func(t *testing.T) {
		rect := screenRect{X: 0, Y: 0, Width: 3840, Height: 2160}
		if err := validateScreenCoords(400, 300, rect); err != nil {
			t.Errorf("unexpected error for in-bounds: %v", err)
		}
	})

	t.Run("in bounds on secondary monitor (negative origin)", func(t *testing.T) {
		rect := screenRect{X: -1920, Y: 0, Width: 3840, Height: 2160}
		if err := validateScreenCoords(-1410, 301, rect); err != nil {
			t.Errorf("unexpected error for negative-x in-bounds: %v", err)
		}
	})

	t.Run("in bounds with virtual desk taller than primary", func(t *testing.T) {
		rect := screenRect{X: 0, Y: 0, Width: 3840, Height: 4320}
		if err := validateScreenCoords(480, 3976, rect); err != nil {
			t.Errorf("unexpected error for large-y in-bounds: %v", err)
		}
	})

	t.Run("out of bounds x too large", func(t *testing.T) {
		rect := screenRect{X: 0, Y: 0, Width: 3840, Height: 2160}
		if err := validateScreenCoords(4000, 300, rect); err == nil {
			t.Error("expected error for out-of-bounds x")
		}
	})

	t.Run("out of bounds y too large", func(t *testing.T) {
		rect := screenRect{X: 0, Y: 0, Width: 3840, Height: 2160}
		if err := validateScreenCoords(400, 2200, rect); err == nil {
			t.Error("expected error for out-of-bounds y")
		}
	})

	t.Run("below virtual origin rejected", func(t *testing.T) {
		rect := screenRect{X: -1920, Y: 0, Width: 3840, Height: 2160}
		if err := validateScreenCoords(-2000, 100, rect); err == nil {
			t.Error("expected error for x below virtual origin")
		}
	})

	t.Run("boundary x at right edge rejected", func(t *testing.T) {
		rect := screenRect{X: 0, Y: 0, Width: 3840, Height: 2160}
		if err := validateScreenCoords(3840, 300, rect); err == nil {
			t.Error("expected error for x == origin+width")
		}
	})
}

func Test_validateMouseAction(t *testing.T) {
	tests := []struct {
		name    string
		action  game.AgentMouseAction
		wantErr bool
	}{
		{name: "left click valid", action: game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_CLICK},
		{name: "left double click valid", action: game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_DOUBLE_CLICK},
		{name: "right click valid", action: game.AgentMouseAction_AGENT_MOUSE_ACTION_RIGHT_CLICK},
		{name: "right double click valid", action: game.AgentMouseAction_AGENT_MOUSE_ACTION_RIGHT_DOUBLE_CLICK},
		{name: "left right press valid", action: game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_RIGHT_PRESS},
		{name: "unspecified rejected", action: game.AgentMouseAction_AGENT_MOUSE_ACTION_UNSPECIFIED, wantErr: true},
		{name: "unknown value rejected", action: game.AgentMouseAction(99), wantErr: true},
		{name: "negative value rejected", action: game.AgentMouseAction(-1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMouseAction(tt.action)
			if tt.wantErr && err == nil {
				t.Errorf("validateMouseAction(%v) expected error, got nil", tt.action)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateMouseAction(%v) unexpected error: %v", tt.action, err)
			}
		})
	}
}

func Test_actionEventSequence(t *testing.T) {
	tests := []struct {
		name      string
		action    game.AgentMouseAction
		wantFlags []uint32
		wantErr   bool
	}{
		{
			name:      "left click: down then up",
			action:    game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_CLICK,
			wantFlags: []uint32{v2MouseLeftDown, v2MouseLeftUp},
		},
		{
			name:      "left double click: two down-up cycles",
			action:    game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_DOUBLE_CLICK,
			wantFlags: []uint32{v2MouseLeftDown, v2MouseLeftUp, v2MouseLeftDown, v2MouseLeftUp},
		},
		{
			name:      "right click: down then up",
			action:    game.AgentMouseAction_AGENT_MOUSE_ACTION_RIGHT_CLICK,
			wantFlags: []uint32{v2MouseRightDown, v2MouseRightUp},
		},
		{
			name:      "right double click: two down-up cycles",
			action:    game.AgentMouseAction_AGENT_MOUSE_ACTION_RIGHT_DOUBLE_CLICK,
			wantFlags: []uint32{v2MouseRightDown, v2MouseRightUp, v2MouseRightDown, v2MouseRightUp},
		},
		{
			name:      "left right press: both down then both up",
			action:    game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_RIGHT_PRESS,
			wantFlags: []uint32{v2MouseLeftDown, v2MouseRightDown, v2MouseRightUp, v2MouseLeftUp},
		},
		{
			name:    "unspecified rejected",
			action:  game.AgentMouseAction_AGENT_MOUSE_ACTION_UNSPECIFIED,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := actionEventSequence(tt.action)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("actionEventSequence(%v) expected error, got nil", tt.action)
				}
				return
			}
			if err != nil {
				t.Fatalf("actionEventSequence(%v) unexpected error: %v", tt.action, err)
			}
			if len(got) != len(tt.wantFlags) {
				t.Fatalf("actionEventSequence(%v) got %d events, want %d",
					tt.action, len(got), len(tt.wantFlags))
			}
			for i, f := range got {
				if f != tt.wantFlags[i] {
					t.Errorf("actionEventSequence(%v) event[%d] = 0x%x, want 0x%x",
						tt.action, i, f, tt.wantFlags[i])
				}
			}
		})
	}
}

func Test_actionEventSequence_LeftRightPressIsSimultaneous(t *testing.T) {
	got, err := actionEventSequence(game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_RIGHT_PRESS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []uint32{
		v2MouseLeftDown,
		v2MouseRightDown,
		v2MouseRightUp,
		v2MouseLeftUp,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d", len(got), len(want))
	}
	for i, f := range got {
		if f != want[i] {
			t.Errorf("event[%d] = 0x%x, want 0x%x", i, f, want[i])
		}
	}
}
