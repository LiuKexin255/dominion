//go:build !windows

package operation

import (
	"errors"

	"dominion/projects/game"
)

// ExecuteWindowMessageClick is not supported on non-Windows platforms.
// Linux builds link this stub so the desktop package compiles for unit
// testing on the dev host; the WINDOW_MESSAGE mouse path is only meaningful
// against a real Win32 HWND.
func ExecuteWindowMessageClick(hwnd uintptr, action game.MouseClickAction, clientX, clientY int32) error {
	return errors.New("window-message mouse not supported on this platform")
}

// ExecuteWindowMessageMove is not supported on non-Windows platforms.
func ExecuteWindowMessageMove(hwnd uintptr, clientX, clientY int32) error {
	return errors.New("window-message mouse not supported on this platform")
}

// ExecuteKeyboardPress is not supported on non-Windows platforms.
func ExecuteKeyboardPress(hwnd uintptr, key game.KeyboardKey) error {
	return errors.New("keyboard press not supported on this platform")
}
