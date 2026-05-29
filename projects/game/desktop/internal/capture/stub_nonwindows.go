//go:build !windows

package capture

import (
	"context"
	"errors"
)

// WindowRef represents a visible, non-cloaked top-level window.
// Stub for non-Windows platforms.
type WindowRef struct {
	Handle         uintptr
	Title          string
	ProcessID      uint32
	ClientWidthPx  int
	ClientHeightPx int
	ScaleFactor    float64
}

// CapturedImage holds the screenshot data of a window's client area.
// Stub for non-Windows platforms.
type CapturedImage struct {
	Data     []byte
	WidthPx  int
	HeightPx int
	Encoding string
}

// ListWindows is not supported on this platform.
func ListWindows(ctx context.Context) ([]WindowRef, error) {
	return nil, errors.New("not supported on this platform")
}

// CaptureWindow is not supported on this platform.
func CaptureWindow(ctx context.Context, hwnd uintptr) (*CapturedImage, error) {
	return nil, errors.New("not supported on this platform")
}
