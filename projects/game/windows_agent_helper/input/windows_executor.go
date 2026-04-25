//go:build windows

package main

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

const (
	inputMouse = 0

	mouseeventfLeftDown   = 0x0002
	mouseeventfLeftUp     = 0x0004
	mouseeventfRightDown  = 0x0008
	mouseeventfRightUp    = 0x0010
	mouseeventfMiddleDown = 0x0020
	mouseeventfMiddleUp   = 0x0040

	defaultDragSteps = 20
	dragTick         = 10 * time.Millisecond
)

var (
	user32                 = syscall.MustLoadDLL("user32.dll")
	procSetProcessDPIAware = user32.MustFindProc("SetProcessDPIAware")
	procClientToScreen     = user32.MustFindProc("ClientToScreen")
	procSetCursorPos       = user32.MustFindProc("SetCursorPos")
	procSendInput          = user32.MustFindProc("SendInput")
)

type point32 struct {
	X int32
	Y int32
}

type mouseInput struct {
	DX        int32
	DY        int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type input struct {
	Type uint32
	MI   mouseInput
}

// WindowsExecutor executes mouse commands through user32.dll without cgo.
type WindowsExecutor struct {
	heldButtons map[Button]bool
}

// NewWindowsExecutor initializes process DPI awareness before sending absolute screen input.
func NewWindowsExecutor() (*WindowsExecutor, error) {
	procSetProcessDPIAware.Call()
	return &WindowsExecutor{heldButtons: map[Button]bool{}}, nil
}

// Execute executes a validated mouse command.
func (executor *WindowsExecutor) Execute(command *Command) (err error) {
	defer func() {
		if err != nil {
			executor.ReleaseAll()
		}
	}()

	switch command.Action {
	case ActionMouseClick:
		return executor.click(command.Button, command.HWND, command.X, command.Y)
	case ActionMouseDoubleClick:
		if err := executor.click(command.Button, command.HWND, command.X, command.Y); err != nil {
			return err
		}
		return executor.click(command.Button, command.HWND, command.X, command.Y)
	case ActionMouseDrag:
		return executor.drag(command)
	case ActionMouseHover:
		return executor.move(command.HWND, command.X, command.Y)
	case ActionMouseHold:
		return executor.hold(command)
	default:
		return fmt.Errorf("unsupported action: %s", command.Action)
	}
}

// ReleaseAll releases any button that is currently considered held by this process.
func (executor *WindowsExecutor) ReleaseAll() {
	for button, held := range executor.heldButtons {
		if held {
			_ = executor.release(button)
		}
	}
}

func (executor *WindowsExecutor) click(button Button, hwnd uintptr, x int, y int) error {
	if err := executor.move(hwnd, x, y); err != nil {
		return err
	}
	if err := executor.press(button); err != nil {
		return err
	}
	return executor.release(button)
}

func (executor *WindowsExecutor) hold(command *Command) error {
	if err := executor.move(command.HWND, command.X, command.Y); err != nil {
		return err
	}
	if err := executor.press(command.Button); err != nil {
		return err
	}
	time.Sleep(time.Duration(command.DurationMS) * time.Millisecond)
	return executor.release(command.Button)
}

func (executor *WindowsExecutor) drag(command *Command) error {
	if err := executor.move(command.HWND, command.FromX, command.FromY); err != nil {
		return err
	}
	if err := executor.press(command.Button); err != nil {
		return err
	}
	defer executor.ReleaseAll()

	steps := defaultDragSteps
	if command.DurationMS > 0 {
		steps = command.DurationMS / int(dragTick/time.Millisecond)
		if steps < 1 {
			steps = 1
		}
	}
	for step := 1; step <= steps; step++ {
		x := command.FromX + ((command.ToX-command.FromX)*step)/steps
		y := command.FromY + ((command.ToY-command.FromY)*step)/steps
		if err := executor.move(command.HWND, x, y); err != nil {
			return err
		}
		if command.DurationMS > 0 {
			time.Sleep(time.Duration(command.DurationMS/steps) * time.Millisecond)
		}
	}
	return executor.release(command.Button)
}

func (executor *WindowsExecutor) move(hwnd uintptr, x int, y int) error {
	point := point32{X: int32(x), Y: int32(y)}
	if hwnd != 0 {
		result, _, err := procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&point)))
		if result == 0 {
			return fmt.Errorf("ClientToScreen: %w", err)
		}
	}
	result, _, err := procSetCursorPos.Call(uintptr(point.X), uintptr(point.Y))
	if result == 0 {
		return fmt.Errorf("SetCursorPos: %w", err)
	}
	return nil
}

func (executor *WindowsExecutor) press(button Button) error {
	flags, err := downFlag(button)
	if err != nil {
		return err
	}
	if err := sendMouse(flags); err != nil {
		return err
	}
	executor.heldButtons[button] = true
	return nil
}

func (executor *WindowsExecutor) release(button Button) error {
	flags, err := upFlag(button)
	if err != nil {
		return err
	}
	if err := sendMouse(flags); err != nil {
		return err
	}
	executor.heldButtons[button] = false
	return nil
}

func downFlag(button Button) (uint32, error) {
	switch button {
	case ButtonLeft:
		return mouseeventfLeftDown, nil
	case ButtonRight:
		return mouseeventfRightDown, nil
	case ButtonMiddle:
		return mouseeventfMiddleDown, nil
	default:
		return 0, fmt.Errorf("invalid button: %s", button)
	}
}

func upFlag(button Button) (uint32, error) {
	switch button {
	case ButtonLeft:
		return mouseeventfLeftUp, nil
	case ButtonRight:
		return mouseeventfRightUp, nil
	case ButtonMiddle:
		return mouseeventfMiddleUp, nil
	default:
		return 0, fmt.Errorf("invalid button: %s", button)
	}
}

func sendMouse(flags uint32) error {
	inputs := []input{{Type: inputMouse, MI: mouseInput{Flags: flags}}}
	result, _, err := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(input{}),
	)
	if result != uintptr(len(inputs)) {
		return fmt.Errorf("SendInput: %w", err)
	}
	return nil
}
