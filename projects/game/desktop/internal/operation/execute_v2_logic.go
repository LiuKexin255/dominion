package operation

import (
	"fmt"

	"dominion/projects/game"
)

// Win32 MOUSEEVENTF_* constant values used by the v2 executor. Defined in a
// platform-independent file so the action-sequence logic compiles and is
// unit-testable on every platform. The values are plain integers matching
// the Win32 API and are only consumed by SendInput on Windows.
const (
	v2MouseLeftDown  uint32 = 0x0002 // MOUSEEVENTF_LEFTDOWN
	v2MouseLeftUp    uint32 = 0x0004 // MOUSEEVENTF_LEFTUP
	v2MouseRightDown uint32 = 0x0008 // MOUSEEVENTF_RIGHTDOWN
	v2MouseRightUp   uint32 = 0x0010 // MOUSEEVENTF_RIGHTUP
)

// Win32 window-message constants and virtual-key codes used by the
// WINDOW_MESSAGE delivery path (PostMessage to the bound window). Defined
// here so the message-mapping logic is unit-testable on every platform; the
// values are plain integers matching the Win32 API and are only consumed by
// PostMessage on Windows.
//
// Refs:
//
//	WM_*BUTTON*: https://learn.microsoft.com/windows/win32/inputdev/wm-mbuttondown
//	WM_KEYDOWN/UP: https://learn.microsoft.com/windows/win32/inputdev/wm-keydown
//	MK_* (wParam key state): https://learn.microsoft.com/windows/win32/inputdev/wm-lbuttondown
const (
	wmLButtonDown uint32 = 0x0201
	wmLButtonUp   uint32 = 0x0202
	wmRButtonDown uint32 = 0x0204
	wmRButtonUp   uint32 = 0x0205

	wmKeyDown uint32 = 0x0100
	wmKeyUp   uint32 = 0x0101

	mkLButton uintptr = 0x0001
	mkRButton uintptr = 0x0002

	vkF2 uint32 = 0x71 // VK_F2 — Minesweeper "new game"
)

// windowMouseMessage is one Win32 mouse window message paired with its wParam
// key-state flags. The WINDOW_MESSAGE realization PostMessages each entry to
// the bound window; lParam (client coordinates) is computed by the caller.
type windowMouseMessage struct {
	Msg    uint32
	WParam uintptr
}

// effectiveDelivery resolves the proto InputDelivery against the contract's
// "unset means SIMULATE" rule (contracts/input-delivery.md §1). UNSPECIFIED
// collapses to SIMULATE so legacy parts that leave delivery unset keep the
// existing physical-cursor behavior.
func effectiveDelivery(d game.InputDelivery) game.InputDelivery {
	if d == game.InputDelivery_INPUT_DELIVERY_WINDOW_MESSAGE {
		return game.InputDelivery_INPUT_DELIVERY_WINDOW_MESSAGE
	}
	return game.InputDelivery_INPUT_DELIVERY_SIMULATE
}

// IsWindowMessage reports whether delivery selects the occlusion-free
// PostMessage path (no SetCursorPos/SendInput — the OS cursor never moves
// over the target, SC-003/FR-014). Exported so the desktop dispatcher (app.go)
// can route a PartBlock to the message-based realization.
func IsWindowMessage(d game.InputDelivery) bool {
	return effectiveDelivery(d) == game.InputDelivery_INPUT_DELIVERY_WINDOW_MESSAGE
}

// windowMouseMessages returns the ordered Win32 WM_*BUTTON* message sequence
// for a click action under WINDOW_MESSAGE delivery. Each entry pairs the
// message with its wParam key-state flags (MK_LBUTTON/MK_RBUTTON); lParam
// (client coordinate) is added by the caller. LEFT_RIGHT_PRESS (chord) is
// modelled as both buttons down then both up in one operation, mirroring the
// SendInput actionEventSequence ordering.
//
// Ref: https://learn.microsoft.com/windows/win32/inputdev/wm-lbuttondown
func windowMouseMessages(action game.MouseClickAction) ([]windowMouseMessage, error) {
	switch action {
	case game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK:
		return []windowMouseMessage{
			{Msg: wmLButtonDown, WParam: mkLButton},
			{Msg: wmLButtonUp, WParam: 0},
		}, nil
	case game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_DOUBLE_CLICK:
		return []windowMouseMessage{
			{Msg: wmLButtonDown, WParam: mkLButton},
			{Msg: wmLButtonUp, WParam: 0},
			{Msg: wmLButtonDown, WParam: mkLButton},
			{Msg: wmLButtonUp, WParam: 0},
		}, nil
	case game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_CLICK:
		return []windowMouseMessage{
			{Msg: wmRButtonDown, WParam: mkRButton},
			{Msg: wmRButtonUp, WParam: 0},
		}, nil
	case game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_DOUBLE_CLICK:
		return []windowMouseMessage{
			{Msg: wmRButtonDown, WParam: mkRButton},
			{Msg: wmRButtonUp, WParam: 0},
			{Msg: wmRButtonDown, WParam: mkRButton},
			{Msg: wmRButtonUp, WParam: 0},
		}, nil
	case game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS:
		return []windowMouseMessage{
			{Msg: wmLButtonDown, WParam: mkLButton},
			{Msg: wmRButtonDown, WParam: mkLButton | mkRButton},
			{Msg: wmRButtonUp, WParam: mkLButton},
			{Msg: wmLButtonUp, WParam: 0},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported click action for window message: %v", action)
	}
}

// virtualKeyCode maps a KeyAction to its Win32 virtual-key code. Extensible:
// each new KeyAction the desktop can realize gets one entry here.
//
// Ref: https://learn.microsoft.com/windows/win32/inputdev/virtual-key-codes
func virtualKeyCode(key game.KeyAction) (uint32, error) {
	switch key {
	case game.KeyAction_KEY_ACTION_F2:
		return vkF2, nil
	default:
		return 0, fmt.Errorf("unsupported key action: %v", key)
	}
}

// makeLParam packs a client coordinate into the Win32 lParam of a
// WM_*BUTTON* message: the low word is x and the high word is y
// (MAKELPARAM = (y << 16) | (x & 0xFFFF)). Pure integer math, so it lives in
// the platform-independent file and is unit-testable on every platform.
//
// Ref: https://learn.microsoft.com/windows/win32/inputdev/wm-lbuttondown
func makeLParam(x, y int32) uintptr {
	return uintptr(uint32(y)<<16 | (uint32(x) & 0xFFFF))
}

// screenRect describes a screen rectangle in absolute pixel coordinates.
// On multi-monitor systems the virtual desktop origin (X, Y) may be
// negative — e.g. when a secondary monitor sits to the left of or above
// the primary monitor — and Width/Height span every attached monitor.
type screenRect struct {
	X, Y          int32
	Width, Height int32
}

// validateScreenCoords checks that absolute screen coordinates fall within
// the given screen rectangle. The rectangle may have a negative origin
// (multi-monitor virtual desktop). Returns an error whose format matches
// the legacy ValidateBounds output for log-search continuity.
func validateScreenCoords(x, y int32, rect screenRect) error {
	if x < rect.X || x >= rect.X+rect.Width || y < rect.Y || y >= rect.Y+rect.Height {
		return fmt.Errorf("coordinates (%d,%d) out of bounds [%dx%d]", x, y, rect.Width, rect.Height)
	}
	return nil
}

// validateClickAction accepts only the five button-pressing MouseClickAction
// values and rejects UNSPECIFIED/unknown values, so the click path never
// reaches actionEventSequence with an action that would emit an empty or
// undefined flag sequence. MouseClickAction has no MOVE variant — moving the
// cursor is the domain of MouseMovePart/MoveCursor, not the click path.
func validateClickAction(action game.MouseClickAction) error {
	switch action {
	case game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK,
		game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_DOUBLE_CLICK,
		game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_CLICK,
		game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_DOUBLE_CLICK,
		game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS:
		return nil
	default:
		return fmt.Errorf("not a click action: %v", action)
	}
}

// actionEventSequence returns the ordered MOUSEEVENTF flag sequence for the
// given click action. Each entry produces one SendInput mouse event
// dispatched by the Windows caller as a relative click at the cursor's
// current position.
//
// LEFT_RIGHT_PRESS order is left-down, right-down, right-up, left-up,
// modeling a simultaneous press (both buttons held) followed by release.
func actionEventSequence(action game.MouseClickAction) ([]uint32, error) {
	switch action {
	case game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK:
		return []uint32{v2MouseLeftDown, v2MouseLeftUp}, nil
	case game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_DOUBLE_CLICK:
		return []uint32{v2MouseLeftDown, v2MouseLeftUp, v2MouseLeftDown, v2MouseLeftUp}, nil
	case game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_CLICK:
		return []uint32{v2MouseRightDown, v2MouseRightUp}, nil
	case game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_DOUBLE_CLICK:
		return []uint32{v2MouseRightDown, v2MouseRightUp, v2MouseRightDown, v2MouseRightUp}, nil
	case game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS:
		return []uint32{v2MouseLeftDown, v2MouseRightDown, v2MouseRightUp, v2MouseLeftUp}, nil
	default:
		return nil, fmt.Errorf("unsupported click action: %v", action)
	}
}
