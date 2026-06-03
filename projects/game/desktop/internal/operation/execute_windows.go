//go:build windows

package operation

import (
	"syscall"
	"unsafe"
)

var (
	user32DLL     = syscall.NewLazyDLL("user32.dll")
	procSendInput = user32DLL.NewProc("SendInput")
)

// mouseInput holds the data for a mouse input event (INPUT type 0).
type mouseInput struct {
	Type        uint32
	DX          int32
	DY          int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

const (
	inputMouse           = 0
	mouseEventFLeftDown  = 0x0002
	mouseEventFLeftUp    = 0x0004
	mouseEventFRightDown = 0x0008
	mouseEventFRightUp   = 0x0010
	mouseEventFMove      = 0x0001
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
		Type:    inputMouse,
		DX:      screenX,
		DY:      screenY,
		DwFlags: mouseEventFMove | 0x8000, // MOUSEEVENTF_ABSOLUTE | MOUSEEVENTF_MOVE
	}
	sendInput(moveInput)

	// Mouse down
	downInput := mouseInput{
		Type:    inputMouse,
		DX:      screenX,
		DY:      screenY,
		DwFlags: downFlag | 0x8000,
	}
	sendInput(downInput)

	// Mouse up
	upInput := mouseInput{
		Type:    inputMouse,
		DX:      screenX,
		DY:      screenY,
		DwFlags: upFlag | 0x8000,
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
