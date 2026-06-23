//go:build windows

package operation

import (
	"errors"

	"dominion/projects/game"
)

var procGetSystemMetrics = user32DLL.NewProc("GetSystemMetrics")

// GetSystemMetrics indices for primary screen dimensions.
const (
	smCXScreen uintptr = 0
	smCYScreen uintptr = 1
)

// screenDimensions returns the primary monitor's width and height in pixels
// via GetSystemMetrics(SM_CXSCREEN) / GetSystemMetrics(SM_CYSCREEN).
func screenDimensions() (int32, int32, error) {
	cx, _, _ := procGetSystemMetrics.Call(smCXScreen)
	if cx == 0 {
		return 0, 0, errors.New("GetSystemMetrics(SM_CXSCREEN) returned 0")
	}
	cy, _, _ := procGetSystemMetrics.Call(smCYScreen)
	if cy == 0 {
		return 0, 0, errors.New("GetSystemMetrics(SM_CYSCREEN) returned 0")
	}
	return int32(cx), int32(cy), nil
}

// ExecuteMouseAction dispatches a mouse action at the given absolute screen
// coordinates via the Windows SendInput API.
//
// screenX, screenY are absolute screen pixel coordinates. Convert
// screenshot-relative coordinates to screen-absolute coordinates before
// calling this function using ScreenshotToScreenCoords together with the
// target window bounds from capture.CaptureWindowBounds:
//
//	bounds, _ := capture.CaptureWindowBounds(hwnd)
//	sx, sy, _ := ScreenshotToScreenCoords(imgX, imgY, int32(bounds.Left), int32(bounds.Top))
//	err := ExecuteMouseAction(sx, sy, action)
//
// The action is validated, coordinates are bounds-checked against the primary
// screen dimensions via ValidateBounds, then normalized to the 0..65535 range
// required by MOUSEEVENTF_ABSOLUTE before dispatch through SendInput.
//
// This replaces the legacy ExecuteMouseClick, which passed raw pixel
// coordinates directly to MOUSEEVENTF_ABSOLUTE (causing mis-targeted clicks)
// and could not express LEFT_RIGHT_PRESS.
func ExecuteMouseAction(screenX, screenY int32, action game.AgentMouseAction) error {
	if err := validateMouseAction(action); err != nil {
		return err
	}

	screenWidth, screenHeight, err := screenDimensions()
	if err != nil {
		return err
	}

	if err := ValidateBounds(screenX, screenY, screenWidth, screenHeight); err != nil {
		return err
	}

	normX := normalizeAbsolute(screenX, screenWidth)
	normY := normalizeAbsolute(screenY, screenHeight)

	events, err := actionEventSequence(action)
	if err != nil {
		return err
	}

	for _, flag := range events {
		sendInput(mouseInput{
			Type:    inputMouse,
			DX:      normX,
			DY:      normY,
			DwFlags: flag | v2MouseAbsolute,
		})
	}

	return nil
}
