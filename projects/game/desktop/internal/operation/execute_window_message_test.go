package operation

import (
	"testing"

	"dominion/projects/game"
)

// Test_effectiveDelivery covers the "unset means SIMULATE" contract rule
// (contracts/input-delivery.md §1) so legacy parts that leave delivery unset
// keep the existing physical-cursor behavior, and only WINDOW_MESSAGE selects
// the occlusion-free path.
func Test_effectiveDelivery(t *testing.T) {
	tests := []struct {
		name string
		in   game.InputDelivery
		want game.InputDelivery
	}{
		{name: "unspecified collapses to simulate", in: game.InputDelivery_INPUT_DELIVERY_UNSPECIFIED, want: game.InputDelivery_INPUT_DELIVERY_SIMULATE},
		{name: "simulate stays simulate", in: game.InputDelivery_INPUT_DELIVERY_SIMULATE, want: game.InputDelivery_INPUT_DELIVERY_SIMULATE},
		{name: "window message stays window message", in: game.InputDelivery_INPUT_DELIVERY_WINDOW_MESSAGE, want: game.InputDelivery_INPUT_DELIVERY_WINDOW_MESSAGE},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveDelivery(tt.in); got != tt.want {
				t.Errorf("effectiveDelivery(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func Test_IsWindowMessage(t *testing.T) {
	tests := []struct {
		name string
		in   game.InputDelivery
		want bool
	}{
		{name: "unspecified is simulate", in: game.InputDelivery_INPUT_DELIVERY_UNSPECIFIED, want: false},
		{name: "simulate is simulate", in: game.InputDelivery_INPUT_DELIVERY_SIMULATE, want: false},
		{name: "window message is window message", in: game.InputDelivery_INPUT_DELIVERY_WINDOW_MESSAGE, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsWindowMessage(tt.in); got != tt.want {
				t.Errorf("IsWindowMessage(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// Test_windowMouseMessages pins the WM_*BUTTON* message sequence each click
// action produces under WINDOW_MESSAGE delivery. The ordering mirrors the
// SendInput actionEventSequence so the two realizations are observationally
// equivalent from the game's perspective; the chord (LEFT_RIGHT_PRESS) is
// both buttons down then both up in one operation.
func Test_windowMouseMessages(t *testing.T) {
	tests := []struct {
		name    string
		action  game.MouseClickAction
		want    []windowMouseMessage
		wantErr bool
	}{
		{
			name:   "left click: ldown then lup",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
			want: []windowMouseMessage{
				{Msg: wmLButtonDown, WParam: mkLButton},
				{Msg: wmLButtonUp, WParam: 0},
			},
		},
		{
			name:   "right click: rdown then rup",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_CLICK,
			want: []windowMouseMessage{
				{Msg: wmRButtonDown, WParam: mkRButton},
				{Msg: wmRButtonUp, WParam: 0},
			},
		},
		{
			name:   "left double click: two ldown/lup cycles",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_DOUBLE_CLICK,
			want: []windowMouseMessage{
				{Msg: wmLButtonDown, WParam: mkLButton},
				{Msg: wmLButtonUp, WParam: 0},
				{Msg: wmLButtonDown, WParam: mkLButton},
				{Msg: wmLButtonUp, WParam: 0},
			},
		},
		{
			name:   "right double click: two rdown/rup cycles",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_DOUBLE_CLICK,
			want: []windowMouseMessage{
				{Msg: wmRButtonDown, WParam: mkRButton},
				{Msg: wmRButtonUp, WParam: 0},
				{Msg: wmRButtonDown, WParam: mkRButton},
				{Msg: wmRButtonUp, WParam: 0},
			},
		},
		{
			name:   "chord: ldown, rdown, rup, lup (both held, then released)",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS,
			want: []windowMouseMessage{
				{Msg: wmLButtonDown, WParam: mkLButton},
				{Msg: wmRButtonDown, WParam: mkLButton | mkRButton},
				{Msg: wmRButtonUp, WParam: mkLButton},
				{Msg: wmLButtonUp, WParam: 0},
			},
		},
		{
			name:    "unspecified rejected",
			action:  game.MouseClickAction_MOUSE_CLICK_ACTION_UNSPECIFIED,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := windowMouseMessages(tt.action)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("windowMouseMessages(%v) expected error, got nil", tt.action)
				}
				return
			}
			if err != nil {
				t.Fatalf("windowMouseMessages(%v) unexpected error: %v", tt.action, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("windowMouseMessages(%v) got %d messages, want %d", tt.action, len(got), len(tt.want))
			}
			for i, m := range got {
				if m != tt.want[i] {
					t.Errorf("windowMouseMessages(%v)[%d] = %+v, want %+v", tt.action, i, m, tt.want[i])
				}
			}
		})
	}
}

func Test_virtualKeyCode(t *testing.T) {
	t.Run("F2 maps to VK_F2", func(t *testing.T) {
		got, err := virtualKeyCode(game.KeyAction_KEY_ACTION_F2)
		if err != nil {
			t.Fatalf("virtualKeyCode(F2) unexpected error: %v", err)
		}
		if got != vkF2 {
			t.Errorf("virtualKeyCode(F2) = %#x, want %#x", got, vkF2)
		}
	})

	t.Run("unspecified rejected", func(t *testing.T) {
		if _, err := virtualKeyCode(game.KeyAction_KEY_ACTION_UNSPECIFIED); err == nil {
			t.Error("virtualKeyCode(UNSPECIFIED) expected error, got nil")
		}
	})
}

// Test_makeLParam pins the Win32 MAKELPARAM packing used for WM_*BUTTON*
// client-coordinate lParam: low word = x, high word = y.
func Test_makeLParam(t *testing.T) {
	tests := []struct {
		name     string
		x, y     int32
		wantLow  uint32
		wantHigh uint32
	}{
		{name: "origin", x: 0, y: 0, wantLow: 0, wantHigh: 0},
		{name: "cell centre (40,216)", x: 40, y: 216, wantLow: 40, wantHigh: 216},
		{name: "large coords", x: 999, y: 720, wantLow: 999, wantHigh: 720},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := makeLParam(tt.x, tt.y)
			if uint32(got)&0xFFFF != tt.wantLow {
				t.Errorf("makeLParam(%d,%d) low word = %#x, want %#x", tt.x, tt.y, uint32(got)&0xFFFF, tt.wantLow)
			}
			if uint32(got)>>16 != tt.wantHigh {
				t.Errorf("makeLParam(%d,%d) high word = %#x, want %#x", tt.x, tt.y, uint32(got)>>16, tt.wantHigh)
			}
		})
	}
}
