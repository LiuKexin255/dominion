//go:build windows

package operation

import (
	"fmt"
	"syscall"

	"dominion/projects/game"
)

// PostMessage realization of the WINDOW_MESSAGE delivery path. PostMessages
// mouse/key messages to the bound window's HWND WITHOUT moving the OS cursor
// (no SetCursorPos/SendInput), so input is occlusion-free (FR-014, SC-003).
//
// Refs:
//
//	PostMessage: https://learn.microsoft.com/windows/win32/api/winuser/nf-winuser-postmessagea
//	WM_LBUTTONDOWN lParam: https://learn.microsoft.com/windows/win32/inputdev/wm-lbuttondown
var procPostMessage = user32DLL.NewProc("PostMessageW")

// ExecuteWindowMessageMouse PostMessages the WM_*BUTTON* sequence for `action`
// to hwnd at client-relative coordinates (clientX, clientY). The coordinate
// is window-client-relative (the same space the mouse tool's screenshot
// coordinates occupy for a full-window capture), supplied by the companion
// MouseMovePart in the same PartBlock. The OS cursor is never moved.
//
// contracts/input-delivery.md §4: a WINDOW_MESSAGE click must be accompanied
// by a coordinate source; the caller (app.go) enforces that precondition.
func ExecuteWindowMessageMouse(hwnd uintptr, clientX, clientY int32, action game.MouseClickAction) error {
	if hwnd == 0 {
		return fmt.Errorf("window message mouse: no window bound")
	}
	messages, err := windowMouseMessages(action)
	if err != nil {
		return err
	}
	lParam := makeLParam(clientX, clientY)
	for _, m := range messages {
		if err := postMessage(hwnd, m.Msg, m.WParam, lParam); err != nil {
			return fmt.Errorf("window message mouse %v: %w", action, err)
		}
	}
	return nil
}

// ExecuteKeyMessage PostMessages WM_KEYDOWN then WM_KEYUP for `key` to hwnd,
// realizing a KeyPart (e.g. F2 "new game") without moving the cursor.
// contracts/input-delivery.md §2: KeyPart declares the operation; the desktop
// owns the implementation (PostMessage WM_KEYDOWN/UP).
func ExecuteKeyMessage(hwnd uintptr, key game.KeyAction) error {
	if hwnd == 0 {
		return fmt.Errorf("window message key: no window bound")
	}
	vk, err := virtualKeyCode(key)
	if err != nil {
		return err
	}
	if err := postMessage(hwnd, wmKeyDown, uintptr(vk), 0); err != nil {
		return fmt.Errorf("window message key %v down: %w", key, err)
	}
	if err := postMessage(hwnd, wmKeyUp, uintptr(vk), 0); err != nil {
		return fmt.Errorf("window message key %v up: %w", key, err)
	}
	return nil
}

// postMessage wraps user32!PostMessageW. PostMessage is asynchronous (returns
// without waiting for the target window to process the message), which is the
// right fit for input injection: the bound game window dispatches the posted
// message from its own message queue on its own thread.
func postMessage(hwnd uintptr, msg uint32, wParam, lParam uintptr) error {
	ret, _, lastErr := procPostMessage.Call(hwnd, uintptr(msg), wParam, lParam)
	if ret == 0 {
		// PostMessage returns 0 on failure (e.g. invalid thread/posted-queue
		// quota). GetLastError context is surfaced when available.
		if lastErr != nil && lastErr != syscall.Errno(0) {
			return lastErr
		}
		return fmt.Errorf("PostMessage(%#x) returned 0", msg)
	}
	return nil
}
