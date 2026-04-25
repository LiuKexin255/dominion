package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const maxHoldDurationMS = 30000

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

// ResponseStatus describes the status value written to stdout.
type ResponseStatus string

const (
	// StatusOK means the command executed successfully.
	StatusOK ResponseStatus = "ok"
	// StatusError means the command failed validation or execution.
	StatusError ResponseStatus = "error"
)

// Command is the validated JSON IPC request accepted from stdin.
type Command struct {
	Action     Action  `json:"action"`
	Button     Button  `json:"button"`
	X          int     `json:"x"`
	Y          int     `json:"y"`
	FromX      int     `json:"from_x"`
	FromY      int     `json:"from_y"`
	ToX        int     `json:"to_x"`
	ToY        int     `json:"to_y"`
	DurationMS int     `json:"duration_ms"`
	HWND       uintptr `json:"hwnd"`
}

// Response is the JSON IPC result written to stdout for each command.
type Response struct {
	Status  ResponseStatus `json:"status"`
	Message string         `json:"message,omitempty"`
}

type rawCommand struct {
	Action     Action  `json:"action"`
	Button     Button  `json:"button"`
	X          *int    `json:"x"`
	Y          *int    `json:"y"`
	FromX      *int    `json:"from_x"`
	FromY      *int    `json:"from_y"`
	ToX        *int    `json:"to_x"`
	ToY        *int    `json:"to_y"`
	DurationMS *int    `json:"duration_ms"`
	HWND       uintptr `json:"hwnd"`
}

// Executor executes one validated command.
type Executor interface {
	Execute(command *Command) error
}

// ExecutorFunc adapts a function to Executor.
type ExecutorFunc func(command *Command) error

// Execute executes command by calling fn.
func (fn ExecutorFunc) Execute(command *Command) error {
	return fn(command)
}

// ParseCommand parses and validates one JSON IPC command.
func ParseCommand(data []byte) (Command, error) {
	raw := new(rawCommand)
	if err := json.Unmarshal(data, raw); err != nil {
		return Command{}, fmt.Errorf("invalid json: %w", err)
	}
	if err := validateAction(raw.Action); err != nil {
		return Command{}, err
	}
	if err := validateButton(raw.Button); err != nil {
		return Command{}, err
	}

	command := Command{
		Action: raw.Action,
		Button: raw.Button,
		HWND:   raw.HWND,
	}
	if raw.DurationMS != nil {
		command.DurationMS = *raw.DurationMS
	}

	switch raw.Action {
	case ActionMouseClick, ActionMouseDoubleClick, ActionMouseHover, ActionMouseHold:
		x, y, err := point(raw.X, raw.Y)
		if err != nil {
			return Command{}, err
		}
		command.X = x
		command.Y = y
	case ActionMouseDrag:
		fromX, fromY, toX, toY, err := dragPoints(raw)
		if err != nil {
			return Command{}, err
		}
		command.FromX = fromX
		command.FromY = fromY
		command.ToX = toX
		command.ToY = toY
	}

	if err := validateDuration(raw.Action, raw.DurationMS); err != nil {
		return Command{}, err
	}
	return command, nil
}

// HandleCommand validates and executes one raw command, returning a JSON response value.
func HandleCommand(data []byte, executor Executor) Response {
	command, err := ParseCommand(data)
	if err != nil {
		return Response{Status: StatusError, Message: err.Error()}
	}
	if err := executor.Execute(&command); err != nil {
		return Response{Status: StatusError, Message: err.Error()}
	}
	return Response{Status: StatusOK}
}

// RunIPC reads newline-delimited JSON commands and writes newline-delimited JSON responses.
func RunIPC(input io.Reader, output io.Writer, executor Executor) error {
	scanner := bufio.NewScanner(input)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		response := HandleCommand(scanner.Bytes(), executor)
		if err := encoder.Encode(&response); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read command: %w", err)
	}
	return nil
}

func validateAction(action Action) error {
	if action == "" {
		return errors.New("missing action")
	}
	switch action {
	case ActionMouseClick, ActionMouseDoubleClick, ActionMouseDrag, ActionMouseHover, ActionMouseHold:
		return nil
	default:
		return fmt.Errorf("invalid action: %s", action)
	}
}

func validateButton(button Button) error {
	if button == "" {
		return errors.New("missing button")
	}
	switch button {
	case ButtonLeft, ButtonRight, ButtonMiddle:
		return nil
	default:
		return fmt.Errorf("invalid button: %s", button)
	}
}

func point(xValue *int, yValue *int) (int, int, error) {
	if xValue == nil {
		return 0, 0, errors.New("missing x")
	}
	if yValue == nil {
		return 0, 0, errors.New("missing y")
	}
	if err := validateCoordinate("x", *xValue); err != nil {
		return 0, 0, err
	}
	if err := validateCoordinate("y", *yValue); err != nil {
		return 0, 0, err
	}
	return *xValue, *yValue, nil
}

func dragPoints(raw *rawCommand) (int, int, int, int, error) {
	if raw.FromX == nil {
		return 0, 0, 0, 0, errors.New("missing from_x")
	}
	if raw.FromY == nil {
		return 0, 0, 0, 0, errors.New("missing from_y")
	}
	if raw.ToX == nil {
		return 0, 0, 0, 0, errors.New("missing to_x")
	}
	if raw.ToY == nil {
		return 0, 0, 0, 0, errors.New("missing to_y")
	}
	coordinates := map[string]int{
		"from_x": *raw.FromX,
		"from_y": *raw.FromY,
		"to_x":   *raw.ToX,
		"to_y":   *raw.ToY,
	}
	for name, value := range coordinates {
		if err := validateCoordinate(name, value); err != nil {
			return 0, 0, 0, 0, err
		}
	}
	return *raw.FromX, *raw.FromY, *raw.ToX, *raw.ToY, nil
}

func validateCoordinate(name string, value int) error {
	if value < 0 {
		return fmt.Errorf("%s must be non-negative", name)
	}
	return nil
}

func validateDuration(action Action, duration *int) error {
	if action == ActionMouseHold && duration == nil {
		return errors.New("missing duration_ms")
	}
	if duration == nil {
		return nil
	}
	if *duration < 0 {
		return errors.New("duration_ms must be non-negative")
	}
	if action == ActionMouseHold && *duration > maxHoldDurationMS {
		return fmt.Errorf("duration_ms must be <= %d", maxHoldDurationMS)
	}
	return nil
}
