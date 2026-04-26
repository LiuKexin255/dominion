//go:build !windows

package window

import "errors"

// IsWindowValid reports false on non-Windows platforms.
func IsWindowValid(hwnd uintptr) bool {
	return false
}

// GetWindowRect reports that Win32 window rectangles are unavailable on this platform.
func GetWindowRect(hwnd uintptr) (Rect, error) {
	return Rect{}, errors.New("not supported on this platform")
}
