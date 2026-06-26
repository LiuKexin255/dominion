//go:build windows

package capture

import (
	"context"
	"fmt"
	"image"
	"log"

	"dominion/projects/game/desktop/internal/operation"

	"github.com/kbinani/screenshot"
)

// CapturedImage holds the PNG-encoded screenshot of a window's full window.
type CapturedImage struct {
	Data     []byte `json:"data"`
	WidthPx  int    `json:"widthPx"`
	HeightPx int    `json:"heightPx"`
	Encoding string `json:"encoding"`
}

// CaptureWindow captures the full window of the specified window as a PNG image.
// It validates the window state, captures using screenshot.CaptureRect, and encodes as PNG.
func CaptureWindow(ctx context.Context, hwnd uintptr) (*CapturedImage, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if hwnd == 0 {
		return nil, fmt.Errorf("capture window: hwnd is 0")
	}

	if !isWindow(hwnd) {
		return nil, fmt.Errorf("capture window: hwnd %d no longer exists", hwnd)
	}

	if !isWindowVisible(hwnd) {
		return nil, fmt.Errorf("capture window: hwnd %d is not visible", hwnd)
	}

	if isIconic(hwnd) {
		return nil, fmt.Errorf("capture window: hwnd %d is minimized", hwnd)
	}

	if isCloaked(hwnd) {
		return nil, fmt.Errorf("capture window: hwnd %d is cloaked", hwnd)
	}

	bounds, err := CaptureWindowBounds(hwnd)
	if err != nil {
		return nil, fmt.Errorf("capture window: %w", err)
	}

	img, err := screenshot.CaptureRect(image.Rect(bounds.Left, bounds.Top, bounds.Right, bounds.Bottom))
	if err != nil {
		return nil, fmt.Errorf("capture window: capture rect: %w", err)
	}

	if img == nil {
		return nil, fmt.Errorf("capture window: capture rect returned nil image")
	}

	if err := operation.DrawCursor(img, int32(bounds.Left), int32(bounds.Top)); err != nil {
		log.Printf("capture window: draw cursor: %v", err)
	}

	pngBytes, err := EncodePNG(img)
	if err != nil {
		return nil, fmt.Errorf("capture window: %w", err)
	}

	return &CapturedImage{
		Data:     pngBytes,
		WidthPx:  bounds.Width(),
		HeightPx: bounds.Height(),
		Encoding: "PNG",
	}, nil
}

// isCloaked checks if a window is cloaked (hidden by DWM composition).
func isCloaked(hwnd uintptr) bool {
	return dwmGetWindowAttribute(hwnd, dwmwaCloaked) != dwmNotCloaked
}
