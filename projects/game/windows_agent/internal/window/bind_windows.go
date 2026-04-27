//go:build windows

package window

import (
	"fmt"
	"unsafe"
)

var (
	procIsWindow      = user32.NewProc("IsWindow")
	procGetWindowRect = user32.NewProc("GetWindowRect")
)

type rect32 struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// IsWindowValid checks if an HWND still references an existing window.
func IsWindowValid(hwnd uintptr) bool {
	result, _, _ := procIsWindow.Call(hwnd)
	return result != 0
}

// GetWindowRect returns the current window rectangle.
func GetWindowRect(hwnd uintptr) (Rect, error) {
	var rect rect32
	result, _, err := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	if result == 0 {
		return Rect{}, fmt.Errorf("GetWindowRect: %w", lastError(err))
	}
	return Rect{
		Left:   rect.Left,
		Top:    rect.Top,
		Right:  rect.Right,
		Bottom: rect.Bottom,
	}, nil
}
