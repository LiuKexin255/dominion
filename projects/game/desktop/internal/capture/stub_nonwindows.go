//go:build !windows

package capture

import (
	"context"
	"errors"
)

// WindowRef represents a visible, non-cloaked top-level window.
// Stub for non-Windows platforms.
type WindowRef struct {
	Handle      uintptr `json:"handle"`
	Title       string  `json:"title"`
	ProcessID   uint32  `json:"processID"`
	WidthPx     int     `json:"widthPx"`
	HeightPx    int     `json:"heightPx"`
	ScaleFactor float64 `json:"scaleFactor"`
}

// WindowBounds represents the bounding rectangle of a window.
// Stub for non-Windows platforms.
type WindowBounds struct {
	Left   int
	Top    int
	Right  int
	Bottom int
}

// Width returns the width of the bounding rectangle.
func (b WindowBounds) Width() int {
	return b.Right - b.Left
}

// Height returns the height of the bounding rectangle.
func (b WindowBounds) Height() int {
	return b.Bottom - b.Top
}

// CapturedImage holds the PNG-encoded screenshot of a window's full window.
// Stub for non-Windows platforms.
type CapturedImage struct {
	Data     []byte `json:"data"`
	WidthPx  int    `json:"widthPx"`
	HeightPx int    `json:"heightPx"`
	Encoding string `json:"encoding"`
}

// ListWindows is not supported on this platform.
func ListWindows(ctx context.Context) ([]WindowRef, error) {
	return nil, errors.New("not supported on this platform")
}

// CaptureWindow is not supported on this platform.
func CaptureWindow(ctx context.Context, hwnd uintptr) (*CapturedImage, error) {
	return nil, errors.New("not supported on this platform")
}

// CaptureWindowBounds is not supported on this platform.
func CaptureWindowBounds(hwnd uintptr) (WindowBounds, error) {
	return WindowBounds{}, errors.New("not supported on this platform")
}

// SetForeground is not supported on non-Windows platforms.
func SetForeground(hwnd uintptr) bool { return false }

// ForegroundWindow is not supported on non-Windows platforms.
func ForegroundWindow() uintptr { return 0 }
