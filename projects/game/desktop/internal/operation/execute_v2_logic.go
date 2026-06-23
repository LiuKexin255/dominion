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
	v2MouseAbsolute  uint32 = 0x8000 // MOUSEEVENTF_ABSOLUTE
	v2MouseLeftDown  uint32 = 0x0002 // MOUSEEVENTF_LEFTDOWN
	v2MouseLeftUp    uint32 = 0x0004 // MOUSEEVENTF_LEFTUP
	v2MouseRightDown uint32 = 0x0008 // MOUSEEVENTF_RIGHTDOWN
	v2MouseRightUp   uint32 = 0x0010 // MOUSEEVENTF_RIGHTUP
)

// mouseAbsoluteMax is the upper bound of the normalized coordinate range
// expected by MOUSEEVENTF_ABSOLUTE. Pixel 0 maps to 0 and pixel (dim-1)
// maps to mouseAbsoluteMax, giving precise edge-to-edge coverage across
// the full primary screen.
const mouseAbsoluteMax int32 = 65535

// normalizeAbsolute maps a pixel coordinate to the 0..65535 range required
// by MOUSEEVENTF_ABSOLUTE. Returns 0 for degenerate dimensions (dim <= 1)
// to avoid division by zero; callers must reject such dimensions before
// reaching this function.
func normalizeAbsolute(pixel, screenDim int32) int32 {
	if screenDim <= 1 {
		return 0
	}
	return (pixel * mouseAbsoluteMax) / (screenDim - 1)
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
