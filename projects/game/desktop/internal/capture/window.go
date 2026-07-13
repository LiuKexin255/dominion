//go:build windows

package capture

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"syscall"
)

// WindowRef represents a visible, non-cloaked top-level window.
type WindowRef struct {
	Handle      uintptr `json:"handle"`
	Title       string  `json:"title"`
	ProcessID   uint32  `json:"processID"`
	WidthPx     int     `json:"widthPx"`
	HeightPx    int     `json:"heightPx"`
	ScaleFactor float64 `json:"scaleFactor"`
}

// WindowBounds represents the bounding rectangle of a window.
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

// ListWindows enumerates all visible, non-minimized, non-cloaked top-level windows
// with a non-empty title and non-zero window area.
func ListWindows(ctx context.Context) ([]WindowRef, error) {
	var (
		mu      sync.Mutex
		results []WindowRef
	)

	callback := syscall.NewCallback(func(hwnd syscall.Handle, lparam uintptr) uintptr {
		// Check context cancellation.
		select {
		case <-ctx.Done():
			return 0
		default:
		}

		wnd := uintptr(hwnd)

		// Filter 1: not visible.
		if !isWindowVisible(wnd) {
			return 1
		}

		// Filter 2: empty title.
		title := getWindowTextW(wnd)
		if title == "" {
			return 1
		}

		// Filter 3: minimized.
		if isIconic(wnd) {
			return 1
		}

		// Filter 4: cloaked (DWM).
		cloaked := dwmGetWindowAttribute(wnd, dwmwaCloaked)
		if cloaked != dwmNotCloaked {
			return 1
		}

		// Filter 5: window bounds zero area.
		bounds, err := CaptureWindowBounds(wnd)
		if err != nil {
			return 1
		}
		if bounds.Width() <= 0 || bounds.Height() <= 0 {
			return 1
		}

		pid := getWindowThreadProcessId(wnd)

		mu.Lock()
		results = append(results, WindowRef{
			Handle:      wnd,
			Title:       title,
			ProcessID:   pid,
			WidthPx:     bounds.Width(),
			HeightPx:    bounds.Height(),
			ScaleFactor: 1.0,
		})
		mu.Unlock()

		return 1
	})

	if err := enumWindows(callback, 0); err != nil {
		return nil, err
	}

	// Sort by title for deterministic order.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Title < results[j].Title
	})

	return results, nil
}

// CaptureWindowBounds retrieves the bounding rectangle of a window.
// It first attempts DWM extended frame bounds, falling back to GetWindowRect.
func CaptureWindowBounds(hwnd uintptr) (WindowBounds, error) {
	r, err := dwmGetExtendedFrameBounds(hwnd)
	if err == nil && r.Right > r.Left && r.Bottom > r.Top {
		return WindowBounds{
			Left:   int(r.Left),
			Top:    int(r.Top),
			Right:  int(r.Right),
			Bottom: int(r.Bottom),
		}, nil
	}
	dwmDesc := formatRectError("dwm", err, r)

	r, err = getWindowRect(hwnd)
	if err == nil && r.Right > r.Left && r.Bottom > r.Top {
		return WindowBounds{
			Left:   int(r.Left),
			Top:    int(r.Top),
			Right:  int(r.Right),
			Bottom: int(r.Bottom),
		}, nil
	}
	winDesc := formatRectError("getWindowRect", err, r)

	return WindowBounds{}, fmt.Errorf("capture window bounds: %s; %s", dwmDesc, winDesc)
}

// formatRectError formats an error message for a window rect retrieval attempt.
func formatRectError(prefix string, err error, r rect) string {
	if err != nil {
		return fmt.Sprintf("%s: %v", prefix, err)
	}
	return fmt.Sprintf("%s: invalid rect left=%d top=%d right=%d bottom=%d", prefix, r.Left, r.Top, r.Right, r.Bottom)
}

// SetForeground brings hwnd to the foreground so synthetic input dispatched by
// the caller (SendInput mouse events) is delivered to it instead of being
// consumed for window activation. See setForegroundReliably for details.
func SetForeground(hwnd uintptr) bool {
	return setForegroundReliably(hwnd)
}

// ForegroundWindow returns the handle of the current foreground window, or 0
// if there is none. Used for diagnostic logging alongside mouse operations.
func ForegroundWindow() uintptr {
	return getForegroundHwnd()
}
