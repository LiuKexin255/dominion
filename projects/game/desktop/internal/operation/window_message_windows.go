//go:build windows

package operation

import (
	"errors"
	"fmt"

	"dominion/projects/game"
)

// procPostMessage is the user32!PostMessageW entry. PostMessageW is preferred
// over PostMessageA because the desktop already uses UTF-16 elsewhere and the
// message parameters carry no strings — both variants accept the same numeric
// arguments.
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-postmessagea
var procPostMessage = user32DLL.NewProc("PostMessageW")

// ExecuteWindowMessageClick posts WM_* button messages for action to hwnd at
// the given client coordinates. It does NOT move the OS cursor: the
// coordinates are packed into lParam and the target window reads them from
// the posted messages directly (spec 018-saolei-mcp FR-004d).
//
// Action is validated and mapped to a WM_* message sequence per the contract
// (specs/018-saolei-mcp/contracts/proto-operation-contract.md "Desktop
// MouseClickAction → WM_* mapping"). Each message in the sequence is posted
// in order; an error from any PostMessage aborts the remainder.
func ExecuteWindowMessageClick(hwnd uintptr, action game.MouseClickAction, clientX, clientY int32) error {
	if err := validateClickAction(action); err != nil {
		return err
	}
	msgs, err := wmMessageSequence(action)
	if err != nil {
		return err
	}
	lParam := makeLPARAM(clientX, clientY)
	for _, msg := range msgs {
		if err := postMessage(hwnd, msg, 0, lParam); err != nil {
			return fmt.Errorf("post WM_* for action %v: %w", action, err)
		}
	}
	return nil
}

// ExecuteWindowMessageMove posts a single WM_MOUSEMOVE to hwnd at the given
// client coordinates. It does NOT move the OS cursor; the message informs the
// target window of a virtual cursor position only.
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-mousemove
func ExecuteWindowMessageMove(hwnd uintptr, clientX, clientY int32) error {
	lParam := makeLPARAM(clientX, clientY)
	if err := postMessage(hwnd, wmMouseMove, 0, lParam); err != nil {
		return fmt.Errorf("post WM_MOUSEMOVE: %w", err)
	}
	return nil
}

// ExecuteKeyboardPress posts WM_KEYDOWN then WM_KEYUP for the given key to
// hwnd via PostMessageW. This route has no foreground-focus requirement and
// no cursor side effects, matching the window-message mouse path's
// semantics (spec 018-saolei-mcp FR-004a; research.md D4).
//
// The key is mapped to its Win32 virtual-key code by keyboardKeyToVK;
// unsupported keys return an error before any message is posted.
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-keydown
// Ref: https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-keyup
func ExecuteKeyboardPress(hwnd uintptr, key game.KeyboardKey) error {
	vk, err := keyboardKeyToVK(key)
	if err != nil {
		return err
	}
	if err := postMessage(hwnd, wmKeyDown, uintptr(vk), 0); err != nil {
		return fmt.Errorf("post WM_KEYDOWN: %w", err)
	}
	if err := postMessage(hwnd, wmKeyUp, uintptr(vk), 0); err != nil {
		return fmt.Errorf("post WM_KEYUP: %w", err)
	}
	return nil
}

// postMessage is a thin wrapper over user32!PostMessageW that converts the
// BOOL return into a Go error. PostMessageW returns 0 on failure and a
// non-zero value on success; the underlying system error is fetched via the
// lazy-proc's lastErr result.
//
// Ref: https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-postmessagea
func postMessage(hwnd uintptr, msg uint32, wParam, lParam uintptr) error {
	r1, _, lastErr := procPostMessage.Call(hwnd, uintptr(msg), wParam, lParam)
	if r1 == 0 {
		if lastErr != nil {
			return lastErr
		}
		return errors.New("PostMessageW returned 0")
	}
	return nil
}
