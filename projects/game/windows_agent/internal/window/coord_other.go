//go:build !windows

package window

import "errors"

// ClientToScreen reports that Win32 coordinate conversion is unavailable on this platform.
func ClientToScreen(hwnd uintptr, x int32, y int32) (int32, int32, error) {
	return 0, 0, errors.New("not supported on this platform")
}
