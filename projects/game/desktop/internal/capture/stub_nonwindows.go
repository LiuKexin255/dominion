//go:build !windows

package capture

import (
	"context"
	"errors"
)

// WindowRef represents a visible, non-cloaked top-level window.
// Stub for non-Windows platforms.
type WindowRef struct {
	Handle         uintptr  `json:"handle"`
	Title          string   `json:"title"`
	ProcessID      uint32   `json:"processID"`
	ClientWidthPx  int      `json:"clientWidthPx"`
	ClientHeightPx int      `json:"clientHeightPx"`
	ScaleFactor    float64  `json:"scaleFactor"`
}

// CapturedImage holds the screenshot data of a window's client area.
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
