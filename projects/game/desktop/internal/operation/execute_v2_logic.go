package operation

import (
	"fmt"

	"dominion/projects/game"
)

// Win32 MOUSEEVENTF_* constant values used by the v2 executor. Defined in a
// platform-independent file so the action-sequence and normalization logic
// compiles and is unit-testable on every platform. The values are plain
// integers matching the Win32 API and are only consumed by SendInput on
// Windows.
const (
	v2MouseAbsolute    uint32 = 0x8000 // MOUSEEVENTF_ABSOLUTE
	v2MouseVirtualDesk uint32 = 0x4000 // MOUSEEVENTF_VIRTUALDESK — normalize coordinates across the entire virtual desktop (all monitors), not just the primary screen
	v2MouseLeftDown    uint32 = 0x0002 // MOUSEEVENTF_LEFTDOWN
	v2MouseLeftUp      uint32 = 0x0004 // MOUSEEVENTF_LEFTUP
	v2MouseRightDown   uint32 = 0x0008 // MOUSEEVENTF_RIGHTDOWN
	v2MouseRightUp     uint32 = 0x0010 // MOUSEEVENTF_RIGHTUP
)

// mouseAbsoluteMax is the upper bound of the normalized coordinate range
// expected by MOUSEEVENTF_ABSOLUTE. Pixel 0 maps to 0 and pixel (dim-1)
// maps to mouseAbsoluteMax, giving precise edge-to-edge coverage.
const mouseAbsoluteMax int32 = 65535

// screenRect describes a screen rectangle in absolute pixel coordinates.
// On multi-monitor systems the virtual desktop origin (X, Y) may be
// negative — e.g. when a secondary monitor sits to the left of or above
// the primary monitor — and Width/Height span every attached monitor.
type screenRect struct {
	X, Y          int32
	Width, Height int32
}

// normalizeAbsoluteVirtual maps an absolute screen pixel coordinate to the
// 0..65535 range required by MOUSEEVENTF_ABSOLUTE|MOUSEEVENTF_VIRTUALDESK.
// origin is the virtual screen's top-left pixel (may be negative on
// multi-monitor systems); dim is the virtual screen's width or height.
// Returns 0 for degenerate dimensions (dim <= 1) to avoid division by
// zero; callers must reject such dimensions before reaching this function.
func normalizeAbsoluteVirtual(pixel, origin, dim int32) int32 {
	if dim <= 1 {
		return 0
	}
	return ((pixel - origin) * mouseAbsoluteMax) / (dim - 1)
}

// validateScreenCoords checks that absolute screen coordinates fall within
// the given screen rectangle. The rectangle may have a negative origin
// (multi-monitor virtual desktop). Returns an error whose format matches
// the legacy ValidateBounds output for log-search continuity.
func validateScreenCoords(x, y int32, rect screenRect) error {
	if x < rect.X || x >= rect.X+rect.Width || y < rect.Y || y >= rect.Y+rect.Height {
		return fmt.Errorf("coordinates (%d,%d) out of bounds [%dx%d]", x, y, rect.Width, rect.Height)
	}
	return nil
}

// validateMouseAction rejects UNSPECIFIED and any unknown action value so
// the executor never dispatches an event with an undefined flag sequence.
func validateMouseAction(action game.AgentMouseAction) error {
	switch action {
	case game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_CLICK,
		game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_DOUBLE_CLICK,
		game.AgentMouseAction_AGENT_MOUSE_ACTION_RIGHT_CLICK,
		game.AgentMouseAction_AGENT_MOUSE_ACTION_RIGHT_DOUBLE_CLICK,
		game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_RIGHT_PRESS:
		return nil
	default:
		return fmt.Errorf("unsupported mouse action: %v", action)
	}
}

// actionEventSequence returns the ordered MOUSEEVENTF flag sequence for the
// given action. Each entry produces one SendInput mouse event; the Windows
// caller ORs v2MouseAbsolute into every flag before dispatch.
//
// LEFT_RIGHT_PRESS order is left-down, right-down, right-up, left-up,
// modeling a simultaneous press (both buttons held) followed by release.
func actionEventSequence(action game.AgentMouseAction) ([]uint32, error) {
	switch action {
	case game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_CLICK:
		return []uint32{v2MouseLeftDown, v2MouseLeftUp}, nil
	case game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_DOUBLE_CLICK:
		return []uint32{v2MouseLeftDown, v2MouseLeftUp, v2MouseLeftDown, v2MouseLeftUp}, nil
	case game.AgentMouseAction_AGENT_MOUSE_ACTION_RIGHT_CLICK:
		return []uint32{v2MouseRightDown, v2MouseRightUp}, nil
	case game.AgentMouseAction_AGENT_MOUSE_ACTION_RIGHT_DOUBLE_CLICK:
		return []uint32{v2MouseRightDown, v2MouseRightUp, v2MouseRightDown, v2MouseRightUp}, nil
	case game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_RIGHT_PRESS:
		return []uint32{v2MouseLeftDown, v2MouseRightDown, v2MouseRightUp, v2MouseLeftUp}, nil
	default:
		return nil, fmt.Errorf("unsupported mouse action: %v", action)
	}
}
