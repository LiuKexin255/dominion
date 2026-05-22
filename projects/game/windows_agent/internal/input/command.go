// Package input manages the input-helper.exe subprocess and translates gateway
// control requests into the helper's JSON-line IPC protocol.
package input

import (
	"fmt"

	runtimepb "dominion/projects/game/runtime"
)

// IMPORTANT: These types MUST exactly match the helper's protocol defined in:
// projects/game/windows_agent/helper/input/command.go

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

// CommandFromMouseClick converts a proto GameMouseClick to an input Command.
func CommandFromMouseClick(click *runtimepb.GameMouseClick, hwnd uintptr) (Command, error) {
	if click == nil {
		return Command{}, fmt.Errorf("mouse click is nil")
	}
	button, err := protoMouseButton(click.GetButton())
	if err != nil {
		return Command{}, err
	}
	x, y := click.GetX(), click.GetY()
	if x < 0 {
		return Command{}, fmt.Errorf("x must be >= 0")
	}
	if y < 0 {
		return Command{}, fmt.Errorf("y must be >= 0")
	}
	return Command{
		Action: ActionMouseClick,
		Button: button,
		X:      int(x),
		Y:      int(y),
		HWND:   hwnd,
	}, nil
}

// CommandFromMouseDoubleClick converts a proto GameMouseDoubleClick to an input Command.
func CommandFromMouseDoubleClick(dc *runtimepb.GameMouseDoubleClick, hwnd uintptr) (Command, error) {
	if dc == nil {
		return Command{}, fmt.Errorf("mouse double click is nil")
	}
	button, err := protoMouseButton(dc.GetButton())
	if err != nil {
		return Command{}, err
	}
	x, y := dc.GetX(), dc.GetY()
	if x < 0 {
		return Command{}, fmt.Errorf("x must be >= 0")
	}
	if y < 0 {
		return Command{}, fmt.Errorf("y must be >= 0")
	}
	return Command{
		Action: ActionMouseDoubleClick,
		Button: button,
		X:      int(x),
		Y:      int(y),
		HWND:   hwnd,
	}, nil
}

// CommandFromMouseDrag converts a proto GameMouseDrag to an input Command.
func CommandFromMouseDrag(drag *runtimepb.GameMouseDrag, hwnd uintptr) (Command, error) {
	if drag == nil {
		return Command{}, fmt.Errorf("mouse drag is nil")
	}
	button, err := protoMouseButton(drag.GetButton())
	if err != nil {
		return Command{}, err
	}
	fromX, fromY := drag.GetFromX(), drag.GetFromY()
	toX, toY := drag.GetToX(), drag.GetToY()
	if fromX < 0 {
		return Command{}, fmt.Errorf("from_x must be >= 0")
	}
	if fromY < 0 {
		return Command{}, fmt.Errorf("from_y must be >= 0")
	}
	if toX < 0 {
		return Command{}, fmt.Errorf("to_x must be >= 0")
	}
	if toY < 0 {
		return Command{}, fmt.Errorf("to_y must be >= 0")
	}
	return Command{
		Action: ActionMouseDrag,
		Button: button,
		FromX:  int(fromX),
		FromY:  int(fromY),
		ToX:    int(toX),
		ToY:    int(toY),
		HWND:   hwnd,
	}, nil
}

// CommandFromMouseHover converts a proto GameMouseHover to an input Command.
func CommandFromMouseHover(hover *runtimepb.GameMouseHover, hwnd uintptr) (Command, error) {
	if hover == nil {
		return Command{}, fmt.Errorf("mouse hover is nil")
	}
	x, y := hover.GetX(), hover.GetY()
	if x < 0 {
		return Command{}, fmt.Errorf("x must be >= 0")
	}
	if y < 0 {
		return Command{}, fmt.Errorf("y must be >= 0")
	}
	return Command{
		Action: ActionMouseHover,
		X:      int(x),
		Y:      int(y),
		HWND:   hwnd,
	}, nil
}

// CommandFromMouseHold converts a proto GameMouseHold to an input Command.
func CommandFromMouseHold(hold *runtimepb.GameMouseHold, hwnd uintptr) (Command, error) {
	if hold == nil {
		return Command{}, fmt.Errorf("mouse hold is nil")
	}
	button, err := protoMouseButton(hold.GetButton())
	if err != nil {
		return Command{}, err
	}
	x, y := hold.GetX(), hold.GetY()
	if x < 0 {
		return Command{}, fmt.Errorf("x must be >= 0")
	}
	if y < 0 {
		return Command{}, fmt.Errorf("y must be >= 0")
	}
	durationMs := hold.GetDurationMs()
	if durationMs <= 0 {
		return Command{}, fmt.Errorf("duration_ms must be > 0")
	}
	if durationMs > MaxHoldDurationMS {
		return Command{}, fmt.Errorf("duration_ms %d exceeds maximum %d", durationMs, MaxHoldDurationMS)
	}
	return Command{
		Action:     ActionMouseHold,
		Button:     button,
		X:          int(x),
		Y:          int(y),
		DurationMS: int(durationMs),
		HWND:       hwnd,
	}, nil
}

// protoMouseButton converts a proto GameMouseButton to an input Button.
func protoMouseButton(button runtimepb.GameMouseButton) (Button, error) {
	switch button {
	case runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_LEFT:
		return ButtonLeft, nil
	case runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_RIGHT:
		return ButtonRight, nil
	case runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_MIDDLE:
		return ButtonMiddle, nil
	default:
		return "", fmt.Errorf("unsupported mouse button: %v", button)
	}
}
