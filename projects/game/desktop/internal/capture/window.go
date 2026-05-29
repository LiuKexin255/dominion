//go:build windows

package capture

import (
	"context"
	"sort"
	"sync"
	"syscall"
)

// WindowRef represents a visible, non-cloaked top-level window.
type WindowRef struct {
	Handle         uintptr
	Title          string
	ProcessID      uint32
	ClientWidthPx  int
	ClientHeightPx int
	ScaleFactor    float64
}

// ListWindows enumerates all visible, non-minimized, non-cloaked top-level windows
// with a non-empty title and non-zero client area.
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

		// Filter 5: client rect zero area.
		r := getClientRect(wnd)
		width := int(r.Right - r.Left)
		height := int(r.Bottom - r.Top)
		if width <= 0 || height <= 0 {
			return 1
		}

		pid := getWindowThreadProcessId(wnd)

		mu.Lock()
		results = append(results, WindowRef{
			Handle:         wnd,
			Title:          title,
			ProcessID:      pid,
			ClientWidthPx:  width,
			ClientHeightPx: height,
			ScaleFactor:    1.0,
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
