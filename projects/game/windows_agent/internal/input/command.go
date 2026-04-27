// Package input manages the input-helper.exe subprocess and translates gateway
// control requests into the helper's JSON-line IPC protocol.
package input

import (
	"fmt"

	gw "dominion/projects/game/gateway/domain"
)

// IMPORTANT: These types MUST exactly match the helper's protocol defined in:
// projects/game/windows_agent_helper/input/command.go

// Action describes a mouse action accepted by the input helper JSON protocol.
type Action string

const (
	// ActionMouseClick presses and releases one mouse button.
	ActionMouseClick Action = "mouse_click"
	// ActionMouseDoubleClick performs two click operations at one point.
	ActionMouseDoubleClick Action = "mouse_double_click"
	// ActionMouseDrag moves with a button held from one point to another.
	ActionMouseDrag Action = "mouse_drag"
	// ActionMouseHover moves the cursor without pressing a button.
	ActionMouseHover Action = "mouse_hover"
	// ActionMouseHold presses and holds a button for a bounded duration.
	ActionMouseHold Action = "mouse_hold"
)

// Button describes the mouse button used by button-based actions.
type Button string

const (
	// ButtonLeft is the primary mouse button.
	ButtonLeft Button = "left"
	// ButtonRight is the secondary mouse button.
	ButtonRight Button = "right"
	// ButtonMiddle is the wheel mouse button.
	ButtonMiddle Button = "middle"
)

// Command is the JSON IPC request sent to input-helper via stdin.
type Command struct {
	Action     Action  `json:"action"`
	Button     Button  `json:"button"`
	X          int     `json:"x,omitempty"`
	Y          int     `json:"y,omitempty"`
	FromX      int     `json:"from_x,omitempty"`
	FromY      int     `json:"from_y,omitempty"`
	ToX        int     `json:"to_x,omitempty"`
	ToY        int     `json:"to_y,omitempty"`
	DurationMS int     `json:"duration_ms,omitempty"`
	HWND       uintptr `json:"hwnd"`
}

// MaxHoldDurationMS is the maximum allowed hold duration in milliseconds.
// This must match the helper's maxHoldDurationMS constant.
const MaxHoldDurationMS = 30000

// ConvertControlRequest converts a gateway ControlRequestPayload to a helper
// Command ready for JSON serialization over the IPC pipe.
func ConvertControlRequest(payload *gw.ControlRequestPayload, hwnd uintptr) (Command, error) {
	if payload == nil {
		return Command{}, fmt.Errorf("payload is nil")
	}

	action, err := convertAction(payload.Kind)
	if err != nil {
		return Command{}, err
	}

	button, err := convertButton(payload.Button)
	if err != nil {
		return Command{}, err
	}

	cmd := Command{
		Action: action,
		Button: button,
		HWND:   hwnd,
	}

	switch action {
	case ActionMouseClick, ActionMouseDoubleClick, ActionMouseHover, ActionMouseHold:
		cmd.X = int(payload.X)
		cmd.Y = int(payload.Y)
	case ActionMouseDrag:
		cmd.FromX = int(payload.FromX)
		cmd.FromY = int(payload.FromY)
		cmd.ToX = int(payload.ToX)
		cmd.ToY = int(payload.ToY)
	}

	if action == ActionMouseHold {
		cmd.DurationMS = int(payload.DurationMs)
		if cmd.DurationMS <= 0 {
			return Command{}, fmt.Errorf("hold requires positive duration_ms, got %d", cmd.DurationMS)
		}
		if cmd.DurationMS > MaxHoldDurationMS {
			return Command{}, fmt.Errorf("duration_ms %d exceeds maximum %d", cmd.DurationMS, MaxHoldDurationMS)
		}
	}

	return cmd, nil
}

// convertAction maps a domain OperationKind to a helper Action.
func convertAction(kind gw.OperationKind) (Action, error) {
	switch kind {
	case gw.OperationKindMouseClick:
		return ActionMouseClick, nil
	case gw.OperationKindMouseDoubleClick:
		return ActionMouseDoubleClick, nil
	case gw.OperationKindMouseDrag:
		return ActionMouseDrag, nil
	case gw.OperationKindMouseHover:
		return ActionMouseHover, nil
	case gw.OperationKindMouseHold:
		return ActionMouseHold, nil
	default:
		return "", fmt.Errorf("unsupported operation kind: %s", kind)
	}
}

// convertButton maps a button string to a helper Button.
func convertButton(button string) (Button, error) {
	switch Button(button) {
	case ButtonLeft, ButtonRight, ButtonMiddle:
		return Button(button), nil
	default:
		return "", fmt.Errorf("unsupported button: %s", button)
	}
}
