package main

import (
	"fmt"

	"dominion/projects/game"
	"dominion/projects/game/desktop/internal/capture"
	"dominion/projects/game/desktop/internal/operation"
)

// runMouseMoveAndClick dispatches a MouseMoveAndClickPart on its method field
// and returns the action label and error.
//
// WINDOW_MESSAGE posts WM_* messages to the bound HWND with the part's coords
// packed into lParam (FR-004d); no OS cursor movement, no screen conversion.
// SIMULATED (and UNSPECIFIED → SIMULATED) reuses the existing
// SetCursorPos + SendInput path: screenshot-relative coords are converted to
// screen-absolute via the bound window's bounds, then move + click fire
// against the cursor's resulting position.
//
// The SIMULATED path inherits the foreground-activation quirk of synthetic
// clicks (SendInput is consumed by window activation when the target is not
// foreground), so SetForeground is called between the move and the click.
func (a *App) runMouseMoveAndClick(part *game.MouseMoveAndClickPart, corrID string) (string, error) {
	switch operation.EffectiveMethod(part.GetMethod()) {
	case game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE:
		label := "move_and_click(window_message):" + part.GetClick().String()
		if err := operation.ExecuteWindowMessageClick(a.boundWin.Handle, part.GetClick(), part.GetXPx(), part.GetYPx()); err != nil {
			return label, fmt.Errorf("window-message click: %w", err)
		}
		a.logger.Info("backend", "mouse action executed", map[string]any{
			"tool_id":        part.GetToolId(),
			"correlation_id": corrID,
			"action":         label,
			"method":         "window_message",
			"window_handle":  a.boundWin.Handle,
			"window_title":   a.boundWin.Title,
			"client_x_px":    part.GetXPx(),
			"client_y_px":    part.GetYPx(),
		})
		return label, nil
	default:
		label := "move_and_click:" + part.GetClick().String()
		bounds, bErr := capture.CaptureWindowBounds(a.boundWin.Handle)
		if bErr != nil {
			return label, fmt.Errorf("capture window bounds: %w", bErr)
		}
		screenX, screenY, cErr := operation.ScreenshotToScreenCoords(part.GetXPx(), part.GetYPx(), int32(bounds.Left), int32(bounds.Top))
		if cErr != nil {
			return label, fmt.Errorf("coordinate conversion: %w", cErr)
		}
		if err := operation.MoveCursor(screenX, screenY); err != nil {
			return label, fmt.Errorf("move cursor: %w", err)
		}
		a.logSetForeground(part.GetToolId(), corrID)
		if err := operation.ExecuteClickAtCurrentPos(part.GetClick()); err != nil {
			return label, fmt.Errorf("click action: %w", err)
		}
		a.logger.Info("backend", "mouse action executed", map[string]any{
			"tool_id":         part.GetToolId(),
			"correlation_id":  corrID,
			"action":          label,
			"method":          "simulated",
			"window_handle":   a.boundWin.Handle,
			"window_title":    a.boundWin.Title,
			"screenshot_x_px": part.GetXPx(),
			"screenshot_y_px": part.GetYPx(),
			"screen_x":        screenX,
			"screen_y":        screenY,
			"window_bounds": map[string]int{
				"left":   bounds.Left,
				"top":    bounds.Top,
				"right":  bounds.Right,
				"bottom": bounds.Bottom,
				"width":  bounds.Right - bounds.Left,
				"height": bounds.Bottom - bounds.Top,
			},
		})
		return label, nil
	}
}

// runMouseMove dispatches a MouseMovePart on its method field and returns the
// action label and error.
//
// WINDOW_MESSAGE posts a single WM_MOUSEMOVE to the bound HWND with the
// part's client coords; no OS cursor movement. SIMULATED converts
// screenshot-relative coords to screen-absolute via the bound window's
// bounds and repositions the cursor (existing behavior).
func (a *App) runMouseMove(part *game.MouseMovePart, corrID string) (string, error) {
	switch operation.EffectiveMethod(part.GetMethod()) {
	case game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE:
		label := "move(window_message)"
		if err := operation.ExecuteWindowMessageMove(a.boundWin.Handle, part.GetXPx(), part.GetYPx()); err != nil {
			return label, fmt.Errorf("window-message move: %w", err)
		}
		a.logger.Info("backend", "mouse action executed", map[string]any{
			"tool_id":        part.GetToolId(),
			"correlation_id": corrID,
			"action":         label,
			"method":         "window_message",
			"window_handle":  a.boundWin.Handle,
			"window_title":   a.boundWin.Title,
			"client_x_px":    part.GetXPx(),
			"client_y_px":    part.GetYPx(),
		})
		return label, nil
	default:
		label := "move"
		bounds, bErr := capture.CaptureWindowBounds(a.boundWin.Handle)
		if bErr != nil {
			return label, fmt.Errorf("capture window bounds: %w", bErr)
		}
		screenX, screenY, cErr := operation.ScreenshotToScreenCoords(part.GetXPx(), part.GetYPx(), int32(bounds.Left), int32(bounds.Top))
		if cErr != nil {
			return label, fmt.Errorf("coordinate conversion: %w", cErr)
		}
		if err := operation.MoveCursor(screenX, screenY); err != nil {
			return label, fmt.Errorf("move cursor: %w", err)
		}
		a.logger.Info("backend", "mouse action executed", map[string]any{
			"tool_id":         part.GetToolId(),
			"correlation_id":  corrID,
			"action":          label,
			"method":          "simulated",
			"window_handle":   a.boundWin.Handle,
			"window_title":    a.boundWin.Title,
			"screenshot_x_px": part.GetXPx(),
			"screenshot_y_px": part.GetYPx(),
			"screen_x":        screenX,
			"screen_y":        screenY,
			"window_bounds": map[string]int{
				"left":   bounds.Left,
				"top":    bounds.Top,
				"right":  bounds.Right,
				"bottom": bounds.Bottom,
				"width":  bounds.Right - bounds.Left,
				"height": bounds.Bottom - bounds.Top,
			},
		})
		return label, nil
	}
}

// runMouseClick dispatches a MouseClickPart on its method field.
//
// WINDOW_MESSAGE is rejected: MouseClickPart carries no coordinates, and the
// WINDOW_MESSAGE path requires client coords to pack into lParam. The
// tool-agnostic protocol pairs WINDOW_MESSAGE clicks with coordinates via
// MouseMoveAndClickPart (spec 018-saolei-mcp FR-004b/FR-004d); a standalone
// MouseClickPart with WINDOW_MESSAGE has no well-defined target.
//
// SIMULATED (and UNSPECIFIED → SIMULATED) preserves the existing behavior:
// the bound window is foregrounded (synthetic clicks are otherwise consumed
// by activation) and button events fire at the cursor's current position.
func (a *App) runMouseClick(part *game.MouseClickPart, corrID string) (string, error) {
	switch operation.EffectiveMethod(part.GetMethod()) {
	case game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE:
		label := "click(window_message)"
		return label, fmt.Errorf("MouseClickPart with WINDOW_MESSAGE method is not supported (no coordinates to pack into lParam)")
	default:
		// Synthetic clicks (SendInput) are consumed by Windows for window
		// activation when the target is not the foreground window, so the
		// bound window must be foreground before the button event fires —
		// otherwise the click lands as an activation gesture with no
		// application-level effect. The cursor position from the preceding
		// mouse_move is preserved by SetForeground.
		label := part.GetClick().String()
		a.logSetForeground(part.GetToolId(), corrID)
		if err := operation.ExecuteClickAtCurrentPos(part.GetClick()); err != nil {
			return label, fmt.Errorf("click action: %w", err)
		}
		return label, nil
	}
}

// logSetForeground foregrounds the bound window and logs the foreground
// transition. Pulled into a helper so the SIMULATED click and move-and-click
// paths share identical behavior. The previous foreground state and the
// SetForeground return value are logged for diagnostic continuity.
func (a *App) logSetForeground(toolID, corrID string) {
	fgBefore := capture.ForegroundWindow()
	fgOk := capture.SetForeground(a.boundWin.Handle)
	fgAfter := capture.ForegroundWindow()
	a.logger.Info("backend", "click: foreground state", map[string]any{
		"tool_id":           toolID,
		"correlation_id":    corrID,
		"window_handle":     a.boundWin.Handle,
		"window_title":      a.boundWin.Title,
		"foreground_before": fgBefore,
		"set_foreground_ok": fgOk,
		"foreground_after":  fgAfter,
	})
}
