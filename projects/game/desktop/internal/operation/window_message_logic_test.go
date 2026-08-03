package operation

import (
	"testing"

	"dominion/projects/game"
)

// Test_makeLPARAM verifies the low-order=x / high-order=y packing of two
// signed 16-bit client coordinates into a Win32 LPARAM. The sign-extension
// within each 16-bit lane is preserved by width-masking to uint16 before
// packing, matching MAKELPARAM.
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-lbuttondown
func Test_makeLPARAM(t *testing.T) {
	tests := []struct {
		name string
		x    int32
		y    int32
		want uintptr
	}{
		{
			name: "zero origin",
			x:    0,
			y:    0,
			want: 0,
		},
		{
			// The saolei fixed geometry's first-cell center: x = 24 + 0*32 + 16
			// = 40, y = 200 + 0*32 + 16 = 216. Verifies a realistic cell
			// center packs correctly.
			name: "saolei cell (0,0) center at (40,216)",
			x:    40,
			y:    216,
			want: uintptr(40) | (uintptr(216) << 16),
		},
		{
			// Cell (3,4) center: x = 24 + 3*32 + 16 = 136, y = 200 + 4*32 +
			// 16 = 344 (matches quickstart.md Scenario 3 click target).
			name: "saolei cell (3,4) center at (136,344)",
			x:    136,
			y:    344,
			want: uintptr(136) | (uintptr(344) << 16),
		},
		{
			// A value >32767 wraps the high bit of the 16-bit lane; on
			// multi-monitor systems this represents a negative signed
			// coordinate. makeLPARAM must mask each lane to 16 bits so the
			// packed value matches what Win32 expects.
			name: "x with high bit set wraps to 16-bit lane",
			x:    0x1FFFF, // 17-bit value; low 16 bits = 0xFFFF
			y:    0,
			want: uintptr(0xFFFF),
		},
		{
			name: "y with high bit set wraps to 16-bit lane",
			x:    0,
			y:    0x1FFFF,
			want: uintptr(0xFFFF) << 16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := makeLPARAM(tt.x, tt.y)
			if got != tt.want {
				t.Errorf("makeLPARAM(%d,%d) = 0x%x, want 0x%x", tt.x, tt.y, got, tt.want)
			}
		})
	}
}

// Test_makeLPARAM_32bitSafety ensures the packed value never exceeds 32 bits
// (uintptr is wider on 64-bit systems but lParam is DWORD). Both lanes
// together must fit in a uint32 — guards against accidental sign propagation
// when y is negative. On every Go-supported architecture uintptr is at most
// 8 bytes wide, so the exact value assertion is universally valid.
func Test_makeLPARAM_32bitSafety(t *testing.T) {
	got := makeLPARAM(-1, -1)
	// x=-1 → uint16 mask = 0xFFFF; y=-1 → uint16 mask = 0xFFFF; packed
	// value = 0xFFFFFFFF, fits in 32 bits.
	if got > 0xFFFFFFFF {
		t.Errorf("makeLPARAM(-1,-1) = 0x%x, must fit in 32 bits", got)
	}
	if got != uintptr(0xFFFFFFFF) {
		t.Errorf("makeLPARAM(-1,-1) = 0x%x, want 0xFFFFFFFF", got)
	}
}

// Test_wmMessageSequence verifies each MouseClickAction maps to the WM_*
// message sequence pinned by the contract
// (specs/018-saolei-mcp/contracts/proto-operation-contract.md "Desktop
// MouseClickAction → WM_* mapping"). LEFT_RIGHT_PRESS must be a single
// simultaneous press: left-down → right-down → right-up → left-up (a chord,
// not two clicks).
func Test_wmMessageSequence(t *testing.T) {
	tests := []struct {
		name    string
		action  game.MouseClickAction
		want    []uint32
		wantErr bool
	}{
		{
			name:   "left click: WM_LBUTTONDOWN then WM_LBUTTONUP",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
			want:   []uint32{wmLButtonDown, wmLButtonUp},
		},
		{
			name:   "left double click: two down/up cycles",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_DOUBLE_CLICK,
			want:   []uint32{wmLButtonDown, wmLButtonUp, wmLButtonDown, wmLButtonUp},
		},
		{
			name:   "right click: WM_RBUTTONDOWN then WM_RBUTTONUP",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_CLICK,
			want:   []uint32{wmRButtonDown, wmRButtonUp},
		},
		{
			name:   "right double click: two down/up cycles",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_DOUBLE_CLICK,
			want:   []uint32{wmRButtonDown, wmRButtonUp, wmRButtonDown, wmRButtonUp},
		},
		{
			name:   "left right press (chord): L-down → R-down → R-up → L-up",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS,
			want:   []uint32{wmLButtonDown, wmRButtonDown, wmRButtonUp, wmLButtonUp},
		},
		{
			name:    "unspecified rejected",
			action:  game.MouseClickAction_MOUSE_CLICK_ACTION_UNSPECIFIED,
			wantErr: true,
		},
		{
			name:    "unknown value rejected",
			action:  game.MouseClickAction(99),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := wmMessageSequence(tt.action)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("wmMessageSequence(%v) expected error, got nil", tt.action)
				}
				return
			}
			if err != nil {
				t.Fatalf("wmMessageSequence(%v) unexpected error: %v", tt.action, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("wmMessageSequence(%v) got %d messages, want %d",
					tt.action, len(got), len(tt.want))
			}
			for i, msg := range got {
				if msg != tt.want[i] {
					t.Errorf("wmMessageSequence(%v) message[%d] = 0x%x, want 0x%x",
						tt.action, i, msg, tt.want[i])
				}
			}
		})
	}
}

// Test_wmMessageSequence_LeftRightPressIsChord is the FR-009 regression guard
// for the chord ordering: LEFT_RIGHT_PRESS must NOT be two separate clicks
// (L-down, L-up, R-down, R-up) — that would dispatch two distinct presses
// instead of one simultaneous chord. The contract pins the order to
// L-down → R-down → R-up → L-up.
func Test_wmMessageSequence_LeftRightPressIsChord(t *testing.T) {
	got, err := wmMessageSequence(game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []uint32{wmLButtonDown, wmRButtonDown, wmRButtonUp, wmLButtonUp}
	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d", len(got), len(want))
	}
	for i, msg := range got {
		if msg != want[i] {
			t.Errorf("message[%d] = 0x%x, want 0x%x (chord order must be L-down, R-down, R-up, L-up)", i, msg, want[i])
		}
	}
}

// Test_wmClickPlan verifies the per-message wParam (MK_* virtual-key flags)
// and dwell (down/up pause) plan for each click action. The wParam must
// reflect the cumulative button-down state at each message so the target
// window can detect simultaneous presses — notably the chord, whose second
// button-down must carry MK_LBUTTON|MK_RBUTTON (both held). A dwell marker
// must precede every down→up transition so the executor inserts a realistic
// press window.
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-lbuttondown (wParam MK_* flags)
func Test_wmClickPlan(t *testing.T) {
	tests := []struct {
		name   string
		action game.MouseClickAction
		want   []postedMouseMessage
	}{
		{
			name:   "left click: down(MK_LBUTTON, dwell) then up",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
			want: []postedMouseMessage{
				{msg: wmLButtonDown, wParam: mkLButton, dwell: true},
				{msg: wmLButtonUp, wParam: 0, dwell: false},
			},
		},
		{
			name:   "left double click: two down/dwell/up cycles",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_DOUBLE_CLICK,
			want: []postedMouseMessage{
				{msg: wmLButtonDown, wParam: mkLButton, dwell: true},
				{msg: wmLButtonUp, wParam: 0, dwell: false},
				{msg: wmLButtonDown, wParam: mkLButton, dwell: true},
				{msg: wmLButtonUp, wParam: 0, dwell: false},
			},
		},
		{
			name:   "right click: down(MK_RBUTTON, dwell) then up",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_CLICK,
			want: []postedMouseMessage{
				{msg: wmRButtonDown, wParam: mkRButton, dwell: true},
				{msg: wmRButtonUp, wParam: 0, dwell: false},
			},
		},
		{
			name:   "right double click: two down/dwell/up cycles",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_DOUBLE_CLICK,
			want: []postedMouseMessage{
				{msg: wmRButtonDown, wParam: mkRButton, dwell: true},
				{msg: wmRButtonUp, wParam: 0, dwell: false},
				{msg: wmRButtonDown, wParam: mkRButton, dwell: true},
				{msg: wmRButtonUp, wParam: 0, dwell: false},
			},
		},
		{
			name:   "chord: L-down, R-down(MK_LBUTTON|MK_RBUTTON, dwell), R-up, L-up",
			action: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS,
			want: []postedMouseMessage{
				// Left goes down first; wParam reports only MK_LBUTTON. No
				// dwell — the next message is another down, not an up.
				{msg: wmLButtonDown, wParam: mkLButton, dwell: false},
				// Right goes down while left is held: wParam must report
				// MK_LBUTTON|MK_RBUTTON (both held). This is the entry the
				// target reads to detect a simultaneous press; dwelling here
				// gives it a realistic both-held window before release.
				{msg: wmRButtonDown, wParam: mkLButton | mkRButton, dwell: true},
				// Right released while left still held: MK_LBUTTON only. No
				// dwell — not a down message.
				{msg: wmRButtonUp, wParam: mkLButton, dwell: false},
				// Left released last: no buttons held.
				{msg: wmLButtonUp, wParam: 0, dwell: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := wmClickPlan(tt.action)
			if err != nil {
				t.Fatalf("wmClickPlan(%v) unexpected error: %v", tt.action, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("wmClickPlan(%v) got %d entries, want %d", tt.action, len(got), len(tt.want))
			}
			for i, e := range got {
				if e != tt.want[i] {
					t.Errorf("plan[%d] = {msg:0x%x, wParam:0x%x, dwell:%v}, want {msg:0x%x, wParam:0x%x, dwell:%v}",
						i, e.msg, e.wParam, e.dwell, tt.want[i].msg, tt.want[i].wParam, tt.want[i].dwell)
				}
			}
		})
	}
}

// Test_wmClickPlan_UnsupportedRejected ensures unsupported/unknown click
// actions surface an error rather than producing an empty plan.
func Test_wmClickPlan_UnsupportedRejected(t *testing.T) {
	tests := []struct {
		name   string
		action game.MouseClickAction
	}{
		{name: "unspecified rejected", action: game.MouseClickAction_MOUSE_CLICK_ACTION_UNSPECIFIED},
		{name: "unknown rejected", action: game.MouseClickAction(99)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := wmClickPlan(tt.action)
			if err == nil {
				t.Fatalf("wmClickPlan(%v) expected error, got plan %v", tt.action, got)
			}
		})
	}
}

// Test_keyboardKeyToVK verifies the KeyboardKey enum maps to the right Win32
// virtual-key code. F2 = VK_F2 = 0x71 is the minesweeper new-game shortcut
// (spec 018-saolei-mcp FR-006; research.md D4).
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/inputdev/virtual-key-codes
func Test_keyboardKeyToVK(t *testing.T) {
	tests := []struct {
		name    string
		key     game.KeyboardKey
		want    uint32
		wantErr bool
	}{
		{
			name: "F2 → VK_F2 (0x71)",
			key:  game.KeyboardKey_KEYBOARD_KEY_F2,
			want: vkF2,
		},
		{
			name:    "UNSPECIFIED rejected (no default key)",
			key:     game.KeyboardKey_KEYBOARD_KEY_UNSPECIFIED,
			wantErr: true,
		},
		{
			name:    "unknown value rejected",
			key:     game.KeyboardKey(99),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := keyboardKeyToVK(tt.key)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("keyboardKeyToVK(%v) expected error, got nil", tt.key)
				}
				return
			}
			if err != nil {
				t.Fatalf("keyboardKeyToVK(%v) unexpected error: %v", tt.key, err)
			}
			if got != tt.want {
				t.Errorf("keyboardKeyToVK(%v) = 0x%x, want 0x%x", tt.key, got, tt.want)
			}
		})
	}

	// Explicit pin: VK_F2 must equal 0x71 (catches accidental constant edits).
	if vkF2 != 0x71 {
		t.Errorf("vkF2 constant = 0x%x, want 0x71 (VK_F2)", vkF2)
	}
}

// Test_EffectiveMethod verifies the FR-004c backward-compatibility rule:
// UNSPECIFIED collapses to SIMULATED so legacy mouse Parts (which omit the
// method field, defaulting to UNSPECIFIED) keep their prior SIMULATED
// behavior; SIMULATED and WINDOW_MESSAGE are returned unchanged.
func Test_EffectiveMethod(t *testing.T) {
	tests := []struct {
		name string
		in   game.MouseInputMethod
		want game.MouseInputMethod
	}{
		{
			name: "UNSPECIFIED → SIMULATED (backward compat for legacy Parts)",
			in:   game.MouseInputMethod_MOUSE_INPUT_METHOD_UNSPECIFIED,
			want: game.MouseInputMethod_MOUSE_INPUT_METHOD_SIMULATED,
		},
		{
			name: "SIMULATED unchanged",
			in:   game.MouseInputMethod_MOUSE_INPUT_METHOD_SIMULATED,
			want: game.MouseInputMethod_MOUSE_INPUT_METHOD_SIMULATED,
		},
		{
			name: "WINDOW_MESSAGE unchanged",
			in:   game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE,
			want: game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE,
		},
		{
			// Unknown values are passed through — the proto enum may grow new
			// variants and the desktop should not silently swallow them.
			name: "unknown value passes through",
			in:   game.MouseInputMethod(99),
			want: game.MouseInputMethod(99),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EffectiveMethod(tt.in)
			if got != tt.want {
				t.Errorf("EffectiveMethod(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
