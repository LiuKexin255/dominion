//go:build windows

package window

import (
	"fmt"
	"unsafe"
)

var procClientToScreen = user32.NewProc("ClientToScreen")

type point32 struct {
	X int32
	Y int32
}

// ClientToScreen converts window-relative coordinates to screen coordinates.
func ClientToScreen(hwnd uintptr, x int32, y int32) (int32, int32, error) {
	point := point32{X: x, Y: y}
	result, _, err := procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&point)))
	if result == 0 {
		return 0, 0, fmt.Errorf("ClientToScreen: %w", lastError(err))
	}
	return point.X, point.Y, nil
}
