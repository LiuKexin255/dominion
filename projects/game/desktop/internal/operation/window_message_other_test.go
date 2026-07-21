//go:build !windows

package operation

import (
	"strings"
	"testing"

	"dominion/projects/game"
)

// TestExecuteWindowMessageClick_StubRejects asserts the non-Windows stub
// rejects every supported MouseClickAction with a "not supported" error, so
// the desktop package compiles and unit-tests its routing logic on the Linux
// dev host without a real Win32 HWND.
func TestExecuteWindowMessageClick_StubRejects(t *testing.T) {
	tests := []struct {
		name   string
		action game.MouseClickAction
	}{
		{name: "left click", action: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK},
		{name: "left double click", action: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_DOUBLE_CLICK},
		{name: "right click", action: game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_CLICK},
		{name: "right double click", action: game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_DOUBLE_CLICK},
		{name: "left right press (chord)", action: game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ExecuteWindowMessageClick(0, tt.action, 100, 200)
			if err == nil {
				t.Fatal("ExecuteWindowMessageClick() expected error, got nil")
			}
			if !strings.Contains(err.Error(), "not supported") {
				t.Errorf("ExecuteWindowMessageClick() error = %q, want to contain %q",
					err.Error(), "not supported")
			}
		})
	}
}

// TestExecuteWindowMessageMove_StubRejects covers the WM_MOUSEMOVE non-Windows
// stub.
func TestExecuteWindowMessageMove_StubRejects(t *testing.T) {
	err := ExecuteWindowMessageMove(0, 100, 200)
	if err == nil {
		t.Fatal("ExecuteWindowMessageMove() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("ExecuteWindowMessageMove() error = %q, want to contain %q",
			err.Error(), "not supported")
	}
}

// TestExecuteKeyboardPress_StubRejects covers the WM_KEYDOWN/UP non-Windows
// stub for F2 (saolei_init's new-game key).
func TestExecuteKeyboardPress_StubRejects(t *testing.T) {
	tests := []struct {
		name string
		key  game.KeyboardKey
	}{
		{name: "F2 (saolei_init)", key: game.KeyboardKey_KEYBOARD_KEY_F2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ExecuteKeyboardPress(0, tt.key)
			if err == nil {
				t.Fatal("ExecuteKeyboardPress() expected error, got nil")
			}
			if !strings.Contains(err.Error(), "not supported") {
				t.Errorf("ExecuteKeyboardPress() error = %q, want to contain %q",
					err.Error(), "not supported")
			}
		})
	}
}
