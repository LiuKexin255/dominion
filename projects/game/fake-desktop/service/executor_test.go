package service

import (
	"bytes"
	"testing"

	game "dominion/projects/game"
)

// cellCenter builds a MouseMoveAndClickPart with the client-space centre of
// cell (x, y) per common/js/dsh-plugins/saolei-loop/src/game/geometry.ts
// (center(x) = 24 + x*32 + 16, center(y) = 104 + y*32 + 16).
func cellCenter(toolID string, action game.MouseClickAction, x, y int32) *game.FlowPart {
	return &game.FlowPart{Kind: &game.FlowPart_MouseMoveAndClick{MouseMoveAndClick: &game.MouseMoveAndClickPart{
		ToolId: toolID,
		XPx:    gridOriginXPx + x*cellSizePx + cellSizePx/2,
		YPx:    gridOriginYPx + y*cellSizePx + cellSizePx/2,
		Click:  action,
		Method: game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE,
	}}}
}

func keyPress(toolID string, key game.KeyboardKey) *game.FlowPart {
	return &game.FlowPart{Kind: &game.FlowPart_KeyboardPress{KeyboardPress: &game.KeyboardPressPart{
		ToolId: toolID,
		Key:    key,
	}}}
}

func TestExecutorKeyboardF2ResetsAndReceipts(t *testing.T) {
	e := NewExecutor(testScenario(t), Fault{})

	if got := e.Execute(keyPress("op-1", game.KeyboardKey_KEYBOARD_KEY_F2)); got == nil {
		t.Fatal("Execute(F2) = nil, want a receipt")
	} else {
		if got.GetToolId() != "op-1" {
			t.Errorf("tool_id = %q, want op-1", got.GetToolId())
		}
		if got.GetStatus() != game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED {
			t.Errorf("status = %v, want SUCCEEDED", got.GetStatus())
		}
		if got.GetMessage() != "F2 pressed, new game started" {
			t.Errorf("message = %q", got.GetMessage())
		}
		assertScreenshot(t, got, []byte("init-png"))
	}

	// A non-F2 key succeeds without resetting the model.
	if got := e.Execute(keyPress("op-2", game.KeyboardKey_KEYBOARD_KEY_UNSPECIFIED)); got == nil {
		t.Fatal("Execute(other key) = nil, want a receipt")
	} else if got.GetScreenshot().GetData() == nil {
		t.Error("non-F2 key receipt lost the screenshot")
	}
}

func TestExecutorCellOpReceiptShape(t *testing.T) {
	e := NewExecutor(testScenario(t), Fault{})
	e.Execute(keyPress("init", game.KeyboardKey_KEYBOARD_KEY_F2))

	got := e.Execute(cellCenter("op-9", game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK, 3, 4))
	if got == nil {
		t.Fatal("Execute(click 3,4) = nil, want a receipt")
	}
	if got.GetToolId() != "op-9" {
		t.Errorf("tool_id = %q, want op-9 (the dispatched tool_id is echoed)", got.GetToolId())
	}
	if got.GetStatus() != game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED {
		t.Errorf("status = %v, want SUCCEEDED", got.GetStatus())
	}
	if got.GetMessage() != "click(3,4) executed" {
		t.Errorf("message = %q, want the inverse-mapped grid coordinates", got.GetMessage())
	}
	assertScreenshot(t, got, []byte("clicked-png"))
}

func TestExecutorFaultInjections(t *testing.T) {
	t.Run("omit screenshot", func(t *testing.T) {
		e := NewExecutor(testScenario(t), Fault{OmitScreenshot: true})
		got := e.Execute(keyPress("op-1", game.KeyboardKey_KEYBOARD_KEY_F2))
		if got.GetScreenshot() != nil {
			t.Error("receipt carries a screenshot despite the injection")
		}
		if got.GetStatus() != game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED {
			t.Errorf("status = %v, want SUCCEEDED", got.GetStatus())
		}
	})

	t.Run("failed status", func(t *testing.T) {
		e := NewExecutor(testScenario(t), Fault{ForceFailedStatus: true})
		got := e.Execute(keyPress("op-1", game.KeyboardKey_KEYBOARD_KEY_F2))
		if got.GetStatus() != game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED {
			t.Errorf("status = %v, want FAILED", got.GetStatus())
		}
	})

	t.Run("disconnect after ops", func(t *testing.T) {
		e := NewExecutor(testScenario(t), Fault{DisconnectAfterOps: 2})
		if e.Disconnected() {
			t.Fatal("Disconnected before any receipt")
		}
		e.Execute(keyPress("op-1", game.KeyboardKey_KEYBOARD_KEY_F2))
		if e.Disconnected() {
			t.Fatal("Disconnected after the first receipt")
		}
		e.Execute(cellCenter("op-2", game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK, 0, 0))
		if !e.Disconnected() {
			t.Fatal("still connected after the configured receipt count")
		}
	})
}

func TestExecutorSignalYieldsNoReceipt(t *testing.T) {
	e := NewExecutor(testScenario(t), Fault{})
	signal := &game.FlowPart{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{}}}
	if got := e.Execute(signal); got != nil {
		t.Fatalf("Execute(status signal) = %v, want nil", got)
	}
}

func TestCellOfInverseMapping(t *testing.T) {
	tests := []struct {
		xPx, yPx  int32
		wantX, wY int32
	}{
		{xPx: 24 + 0*32 + 16, yPx: 104 + 0*32 + 16, wantX: 0, wY: 0},
		{xPx: 24 + 3*32 + 16, yPx: 104 + 4*32 + 16, wantX: 3, wY: 4},
		{xPx: 24 + 8*32 + 16, yPx: 104 + 15*32 + 16, wantX: 8, wY: 15},
	}
	for _, tt := range tests {
		gotX, gotY := cellOf(tt.xPx, tt.yPx)
		if gotX != tt.wantX || gotY != tt.wY {
			t.Fatalf("cellOf(%d,%d) = (%d,%d), want (%d,%d)", tt.xPx, tt.yPx, gotX, gotY, tt.wantX, tt.wY)
		}
	}
}

// assertScreenshot decodes the receipt screenshot and compares its bytes to
// the scenario fixture the model should have captured.
func assertScreenshot(t *testing.T, receipt *game.FlowResultPart, want []byte) {
	t.Helper()
	shot := receipt.GetScreenshot()
	if shot == nil {
		t.Fatal("receipt has no screenshot")
	}
	if shot.GetEncoding() != game.ImageEncoding_IMAGE_ENCODING_PNG {
		t.Errorf("encoding = %v, want PNG", shot.GetEncoding())
	}
	if !bytes.Equal(shot.GetData(), want) {
		t.Errorf("screenshot bytes = %q, want %q", shot.GetData(), want)
	}
}
