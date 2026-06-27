//go:build windows

package operation

import (
	"errors"

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

// MoveCursor positions the mouse cursor at the given absolute screen
// coordinates via user32!SetCursorPos. screenX, screenY are absolute screen
// pixel coordinates relative to the virtual desktop, whose origin may be
// negative on multi-monitor systems where a secondary monitor sits to the
// left of or above the primary monitor.
//
// Coordinates are bounds-checked against the virtual desktop rectangle via
// validateScreenCoords before dispatch, so an out-of-bounds target is
// rejected before the cursor is touched.
//
// Convert screenshot-relative coordinates to screen-absolute coordinates
// before calling this function using ScreenshotToScreenCoords together with
// the target window bounds from capture.CaptureWindowBounds:
//
//	bounds, _ := capture.CaptureWindowBounds(hwnd)
//	sx, sy, _ := ScreenshotToScreenCoords(imgX, imgY, int32(bounds.Left), int32(bounds.Top))
//	err := MoveCursor(sx, sy)
//
// This pairs with ExecuteClickAtCurrentPos, which dispatches button events as
// relative clicks at the cursor's current position. Splitting move and click
// lets callers issue a MOVE action (cursor only) or a click action (buttons
// only) without coupling cursor repositioning to the click dispatch path.
func MoveCursor(screenX, screenY int32) error {
	rect, err := virtualScreenRect()
	if err != nil {
		return err
	}

	if err := validateScreenCoords(screenX, screenY, rect); err != nil {
		return err
	}

	return setCursorPos(screenX, screenY)
}

// ExecuteClickAtCurrentPos dispatches button events for the given click
// action at the cursor's current position. It does NOT move the cursor —
// pair it with MoveCursor (or rely on the cursor's existing position) so the
// click lands where intended.
//
// Button events are sent as relative clicks (no MOUSEEVENTF_ABSOLUTE, dx/dy
// left zero) so each event fires at the cursor's current position. This is
// the canonical, widely-deployed Go pattern (github.com/stephen-fox/
// user32util and github.com/go-vgo/robotgo): SetCursorPos (called separately
// via MoveCursor) handles multi-monitor negative coordinates natively and
// avoids the edge cases of MOUSEEVENTF_ABSOLUTE|MOUSEEVENTF_VIRTUALDESK
// normalization, which on some configurations dispatched events that landed
// at incorrect positions despite SendInput returning success.
//
// action must be one of the five MouseClickAction button-pressing values;
// validateClickAction rejects UNSPECIFIED/unknown values before any event is
// dispatched. MouseClickAction carries no MOVE variant — cursor
// repositioning is handled by MoveCursor.
func ExecuteClickAtCurrentPos(action game.MouseClickAction) error {
	if err := validateClickAction(action); err != nil {
		return err
	}

	events, err := actionEventSequence(action)
	if err != nil {
		return err
	}

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
