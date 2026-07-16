//go:build !windows

package operation

import (
	"errors"

	"dominion/projects/game"
)

// ExecuteWindowMessageMouse is not supported on non-Windows platforms. The
// WINDOW_MESSAGE delivery path requires Win32 PostMessage; the stub keeps the
// operation package compiling on dev/build hosts (Linux) where the SIMULATE
// stubs already return "not supported".
func ExecuteWindowMessageMouse(hwnd uintptr, clientX, clientY int32, action game.MouseClickAction) error {
	return errors.New("window message mouse not supported on this platform")
}

// ExecuteKeyMessage is not supported on non-Windows platforms.
func ExecuteKeyMessage(hwnd uintptr, key game.KeyAction) error {
	return errors.New("window message key not supported on this platform")
}
