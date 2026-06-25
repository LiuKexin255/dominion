//go:build windows

package operation

import (
	"errors"
	"syscall"
	"unsafe"
)

var (
	user32DLL        = syscall.NewLazyDLL("user32.dll")
	procSendInput    = user32DLL.NewProc("SendInput")
	procSetCursorPos = user32DLL.NewProc("SetCursorPos")
)

// mouseEvent mirrors the Win32 MOUSEINPUT structure embedded in INPUT.
//
// It is declared as a separate struct so Go's natural alignment rules
// reproduce the Win32 layout on both 32-bit and 64-bit Windows: on x64 the
// trailing uintptr (ULONG_PTR) forces 4 bytes of tail padding inside this
// struct, matching the MSVC MOUSEINPUT layout exactly.
//
// Ref: https://learn.microsoft.com/windows/win32/api/winuser/ns-winuser-mouseinput
type mouseEvent struct {
	Dx          int32
	Dy          int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

// mouseInput mirrors the Win32 INPUT structure for mouse events.
//
// On x64 the embedded mouseEvent (which contains a pointer-sized field) is
// 8-byte aligned, so Go inserts 4 bytes of padding between Type and Mi —
// exactly matching the Win32 INPUT union alignment. Computing the layout via
// the nested struct (rather than flat fields) is what makes SendInput read
// the right bytes: a previous flat layout placed Dx/Dy at offset 4/8 instead
// of 8/12, so Windows read dwFlags from the wrong offset and silently
// dispatched no-op mouse events.
//
// Ref: https://learn.microsoft.com/windows/win32/api/winuser/ns-winuser-input
type mouseInput struct {
	Type uint32
	Mi   mouseEvent
}

const (
	inputMouse           = 0
	mouseEventFLeftDown  = 0x0002
	mouseEventFLeftUp    = 0x0004
	mouseEventFRightDown = 0x0008
	mouseEventFRightUp   = 0x0010
	mouseEventFMove      = 0x0001
	mouseEventFAbsolute  = 0x8000
)

// ExecuteMouseClick performs a mouse click at absolute screen coordinates.
// button: 1=LEFT, 2=RIGHT. clickType: 1=SINGLE, 2=DOUBLE.
func ExecuteMouseClick(screenX, screenY int32, button int32, clickType int32) error {
	clicks := clickType
	if clicks < 1 {
		clicks = 1
	}
	for i := int32(0); i < clicks; i++ {
		if err := sendMouseClick(screenX, screenY, button); err != nil {
			return err
		}
	}
	return nil
}

// ExecuteKeyPress performs a keyboard key press.
// keyCodes is a string representation of key codes.
func ExecuteKeyPress(keyCodes string) error {
	// Stub for step3.a — real implementation uses SendInput with keyboard events.
	_ = keyCodes
	return nil
}

// sendMouseClick sends a single click (down + up) at the given coordinates.
//
// NOTE: this legacy helper uses MOUSEEVENTF_ABSOLUTE without virtual-desk
// normalization, so it is only correct on a single-monitor setup whose
// primary monitor is at (0,0). New callers should go through
// ExecuteMouseAction, which normalizes against the full virtual desktop.
func sendMouseClick(screenX, screenY int32, button int32) error {
	var downFlag, upFlag uint32
	switch button {
	case 2: // RIGHT
		downFlag = mouseEventFRightDown
		upFlag = mouseEventFRightUp
	default: // LEFT
		downFlag = mouseEventFLeftDown
		upFlag = mouseEventFLeftUp
	}

	// Move cursor to position
	moveInput := mouseInput{
		Type: inputMouse,
		Mi: mouseEvent{
			Dx:      screenX,
			Dy:      screenY,
			DwFlags: mouseEventFMove | mouseEventFAbsolute,
		},
	}
	sendInput(moveInput)

	// Mouse down
	downInput := mouseInput{
		Type: inputMouse,
		Mi: mouseEvent{
			Dx:      screenX,
			Dy:      screenY,
			DwFlags: downFlag | mouseEventFAbsolute,
		},
	}
	sendInput(downInput)

	// Mouse up
	upInput := mouseInput{
		Type: inputMouse,
		Mi: mouseEvent{
			Dx:      screenX,
			Dy:      screenY,
			DwFlags: upFlag | mouseEventFAbsolute,
		},
	}
	sendInput(upInput)

	return nil
}

// sendInput sends an input event via the Windows SendInput API.
func sendInput(inp mouseInput) {
	procSendInput.Call(
		uintptr(1),
		uintptr(unsafe.Pointer(&inp)),
		unsafe.Sizeof(inp),
	)
}

// setCursorPos moves the mouse cursor to the given absolute screen pixel
// coordinates via user32!SetCursorPos. Coordinates are raw virtual-screen
// pixels and may be negative on multi-monitor systems where a monitor sits
// to the left of or above the primary monitor; SetCursorPos accepts any
// coordinate inside the virtual screen rectangle.
//
// We use SetCursorPos + relative click (instead of MOUSEEVENTF_ABSOLUTE)
// because that is the canonical, widely-deployed Go pattern (matching
// github.com/stephen-fox/user32util and github.com/go-vgo/robotgo).
// SetCursorPos handles multi-monitor negative coordinates natively and avoids
// the edge cases of MOUSEEVENTF_ABSOLUTE|MOUSEEVENTF_VIRTUALDESK
// normalization, which on some configurations dispatches events that land at
// incorrect positions despite SendInput returning success.
//
// Ref: https://learn.microsoft.com/windows/win32/api/winuser/nf-winuser-setcursorpos
func setCursorPos(x, y int32) error {
	r1, _, lastErr := procSetCursorPos.Call(uintptr(x), uintptr(y))
	if r1 == 0 {
		if lastErr != nil {
			return lastErr
		}
		return errors.New("SetCursorPos returned 0")
	}
	return nil
}
