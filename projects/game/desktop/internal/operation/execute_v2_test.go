package operation

import (
	"testing"

	"dominion/projects/game"
)

func Test_normalizeAbsolute(t *testing.T) {
	tests := []struct {
		name      string
		pixel     int32
		screenDim int32
		want      int32
	}{
		{name: "origin maps to 0", pixel: 0, screenDim: 1920, want: 0},
		{name: "last pixel maps to 65535 at 1920", pixel: 1919, screenDim: 1920, want: 65535},
		{name: "last pixel maps to 65535 at 1080", pixel: 1079, screenDim: 1080, want: 65535},
		{name: "center of 1920", pixel: 960, screenDim: 1920, want: (960 * 65535) / 1919},
		{name: "100px on 800-wide screen", pixel: 100, screenDim: 800, want: (100 * 65535) / 799},
		{name: "degenerate dim 1 returns 0", pixel: 500, screenDim: 1, want: 0},
		{name: "degenerate dim 0 returns 0", pixel: 500, screenDim: 0, want: 0},
		{name: "negative dim returns 0", pixel: 500, screenDim: -10, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeAbsolute(tt.pixel, tt.screenDim)
			if got != tt.want {
				t.Errorf("normalizeAbsolute(%d, %d) = %d, want %d",
					tt.pixel, tt.screenDim, got, tt.want)
			}
		})
	}
}

func Test_normalizeAbsolute_AlwaysInRange(t *testing.T) {
	for dim := int32(2); dim <= 3840; dim += 37 {
		for pixel := int32(0); pixel < dim; pixel += 41 {
			got := normalizeAbsolute(pixel, dim)
			if got < 0 || got > mouseAbsoluteMax {
				t.Errorf("normalizeAbsolute(%d, %d) = %d, outside [0, %d]",
					pixel, dim, got, mouseAbsoluteMax)
			}
		}
	}
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
