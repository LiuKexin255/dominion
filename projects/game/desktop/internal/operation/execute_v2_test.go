package operation

import (
	"testing"

	"dominion/projects/game"
)

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
