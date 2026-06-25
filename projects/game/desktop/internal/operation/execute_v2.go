//go:build windows

package operation

import (
	"errors"
	"fmt"

	"dominion/projects/game"
)

var procGetSystemMetrics = user32DLL.NewProc("GetSystemMetrics")

// GetSystemMetrics indices for the virtual desktop. Unlike SM_CXSCREEN /
// SM_CYSCREEN (which report only the primary monitor), the SM_*VIRTUALSCREEN
// metrics describe the bounding rectangle of every attached monitor. The
// origin (X, Y) is the top-left of the virtual desktop and may be negative
// when a secondary monitor sits to the left of or above the primary monitor.
const (
	smXVirtualScreen  uintptr = 76
	smYVirtualScreen  uintptr = 77
	smCXVirtualScreen uintptr = 78
	smCYVirtualScreen uintptr = 79
)

// virtualScreenRect returns the bounding rectangle of the entire virtual
// desktop via the SM_*VIRTUALSCREEN metrics. This covers all attached
// monitors, so absolute screen coordinates derived from windows on any
// monitor — including those with negative X/Y — fall inside this rectangle.
func virtualScreenRect() (screenRect, error) {
	x, _, _ := procGetSystemMetrics.Call(smXVirtualScreen)
	y, _, _ := procGetSystemMetrics.Call(smYVirtualScreen)
	cx, _, _ := procGetSystemMetrics.Call(smCXVirtualScreen)
	cy, _, _ := procGetSystemMetrics.Call(smCYVirtualScreen)
	if cx == 0 || cy == 0 {
		return screenRect{}, errors.New("GetSystemMetrics(SM_CXVIRTUALSCREEN/SM_CYVIRTUALSCREEN) returned zero area")
	}
	return screenRect{X: int32(x), Y: int32(y), Width: int32(cx), Height: int32(cy)}, nil
}

// ExecuteMouseAction dispatches a mouse action at the given absolute screen
// coordinates using the canonical two-phase pattern:
//
//  1. Move the cursor to (screenX, screenY) with user32!SetCursorPos.
//  2. Send relative button events (no MOUSEEVENTF_ABSOLUTE) so the click
//     fires at the cursor's current position.
//
// This matches the widely-deployed Go pattern (github.com/stephen-fox/
// user32util and github.com/go-vgo/robotgo). SetCursorPos handles
// multi-monitor negative coordinates natively and avoids the edge cases of
// MOUSEEVENTF_ABSOLUTE|MOUSEEVENTF_VIRTUALDESK normalization, which on some
// configurations dispatched events that landed at incorrect positions
// despite SendInput returning success.
//
// screenX, screenY are absolute screen pixel coordinates (relative to the
// virtual desktop, whose origin may be negative on multi-monitor systems).
// Convert screenshot-relative coordinates to screen-absolute coordinates
// before calling this function using ScreenshotToScreenCoords together with
// the target window bounds from capture.CaptureWindowBounds:
//
//	bounds, _ := capture.CaptureWindowBounds(hwnd)
//	sx, sy, _ := ScreenshotToScreenCoords(imgX, imgY, int32(bounds.Left), int32(bounds.Top))
//	err := ExecuteMouseAction(sx, sy, action)
//
// Coordinates are bounds-checked against the virtual desktop rectangle via
// validateScreenCoords before dispatch.
//
// This replaces the legacy ExecuteMouseClick, which passed raw pixel
// coordinates directly to MOUSEEVENTF_ABSOLUTE (causing mis-targeted clicks)
// and could not express LEFT_RIGHT_PRESS.
func ExecuteMouseAction(screenX, screenY int32, action game.AgentMouseAction) error {
	if err := validateMouseAction(action); err != nil {
		return err
	}

	rect, err := virtualScreenRect()
	if err != nil {
		return err
	}

	if err := validateScreenCoords(screenX, screenY, rect); err != nil {
		return err
	}

	// Phase 1: position the cursor at the target screen coordinates. Any
	// subsequent relative mouse event will fire at this position.
	if err := setCursorPos(screenX, screenY); err != nil {
		return fmt.Errorf("set cursor position: %w", err)
	}

	events, err := actionEventSequence(action)
	if err != nil {
		return err
	}

	// Phase 2: dispatch button events as relative clicks (no
	// MOUSEEVENTF_ABSOLUTE, dx/dy left zero) so they act at the cursor's
	// current position set by SetCursorPos above.
	for _, flag := range events {
		sendInput(mouseInput{
			Type: inputMouse,
			Mi: mouseEvent{
				DwFlags: flag,
			},
		})
	}

	return nil
}
