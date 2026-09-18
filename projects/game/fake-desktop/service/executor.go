package service

import (
	"fmt"

	game "dominion/projects/game"
)

// Client-space geometry of the saolei board grid, mirroring the dispatch-side
// formula (common/js/dsh-plugins/saolei-loop/src/game/geometry.ts —
// center(x) = 24 + x*32 + 16, center(y) = 104 + y*32 + 16; the client-space
// origin carries the 96 px chrome offset from the screenshot-space board top
// Y=200, specs/024-tool-render-coord-fix/research.md D1/D2). The executor
// inverse-maps a dispatched cell-centre back to grid coordinates purely for
// the receipt message; execution itself never depends on the coordinates.
const (
	gridOriginXPx = 24
	gridOriginYPx = 104
	cellSizePx    = 32
)

// Fault carries the configured failure injections (research.md D15: 断连 /
// 不回截图 / FAILED 结果). The zero value executes every operation normally.
type Fault struct {
	// DisconnectAfterOps closes the connection once the operation-receipt
	// count reaches N (N > 0 enables the injection).
	DisconnectAfterOps int
	// OmitScreenshot sends operation receipts without the screenshot,
	// driving the agent-side recognition-failure path.
	OmitScreenshot bool
	// ForceFailedStatus replies TOOL_RESULT_STATUS_FAILED to operations.
	ForceFailedStatus bool
}

// Executor turns inbound FlowPart operations into FlowResultPart receipts by
// advancing the deterministic board model. It tracks the receipt count for
// the disconnect injection; Execute is expected to be called from the single
// connection read loop, so no locking is provided.
type Executor struct {
	board *board
	fault Fault
	ops   int
	// disconnected latches once the disconnect injection fired.
	disconnected bool
}

// NewExecutor starts one executor on the given scenario.
func NewExecutor(scenario *Scenario, fault Fault) *Executor {
	return &Executor{board: newBoard(scenario), fault: fault}
}

// Disconnected reports whether the disconnect injection has fired and the
// connection must be torn down.
func (e *Executor) Disconnected() bool { return e.disconnected }

// Execute processes one operation FlowPart and returns its receipt, or nil
// for frames that are not operations (the signal kinds wait/warn/status/
// queue — nothing to execute, nothing to report).
func (e *Executor) Execute(part *game.FlowPart) *game.FlowResultPart {
	switch kind := part.GetKind().(type) {
	case *game.FlowPart_KeyboardPress:
		return e.executeKeyboard(kind.KeyboardPress)
	case *game.FlowPart_MouseMoveAndClick:
		return e.executeCellOp(kind.MouseMoveAndClick)
	case *game.FlowPart_MouseMove:
		return e.receipt(kind.MouseMove.GetToolId(), "mouse move executed")
	case *game.FlowPart_MouseClick:
		return e.receipt(kind.MouseClick.GetToolId(), "mouse click executed")
	default:
		return nil
	}
}

// executeKeyboard handles a keypress. The F2 new-game shortcut resets the
// model; any other key succeeds without touching the board (the model is
// defined only for the F2 new-game semantics).
func (e *Executor) executeKeyboard(press *game.KeyboardPressPart) *game.FlowResultPart {
	if press.GetKey() == game.KeyboardKey_KEYBOARD_KEY_F2 {
		e.board.newGame()
		return e.receipt(press.GetToolId(), "F2 pressed, new game started")
	}
	return e.receipt(press.GetToolId(), "key pressed")
}

// executeCellOp applies one cell operation to the model and reports the grid
// coordinates inverse-mapped from the dispatched client-space centre.
func (e *Executor) executeCellOp(op *game.MouseMoveAndClickPart) *game.FlowResultPart {
	opKind, ok := cellOpKind(op.GetClick())
	if !ok {
		return e.receipt(op.GetToolId(), fmt.Sprintf("click action %s ignored", op.GetClick()))
	}
	x, y := cellOf(op.GetXPx(), op.GetYPx())
	if _, err := e.board.applyCellOp(opKind); err != nil {
		return e.receipt(op.GetToolId(), err.Error())
	}
	return e.receipt(op.GetToolId(), fmt.Sprintf("%s(%d,%d) executed", opKind, x, y))
}

// receipt builds the FlowResultPart: the caller's tool_id echoed back, the
// configured status, and the current board image unless the screenshot
// injection suppresses it.
func (e *Executor) receipt(toolID, message string) *game.FlowResultPart {
	e.ops++
	if e.fault.DisconnectAfterOps > 0 && e.ops >= e.fault.DisconnectAfterOps {
		e.disconnected = true
	}
	status := game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED
	if e.fault.ForceFailedStatus {
		status = game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED
	}
	result := &game.FlowResultPart{
		ToolId:  toolID,
		Status:  status,
		Message: message,
	}
	if !e.fault.OmitScreenshot {
		result.Screenshot = &game.ImagePart{
			Encoding: game.ImageEncoding_IMAGE_ENCODING_PNG,
			Data:     e.board.current(),
		}
	}
	return result
}

// cellOpKind maps a MouseClickAction to the cell-operation kind consumed by
// the board model; ok is false for actions that are not cell operations.
func cellOpKind(action game.MouseClickAction) (string, bool) {
	switch action {
	case game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK:
		return opClick, true
	case game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_CLICK:
		return opFlag, true
	case game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS:
		return opChord, true
	default:
		return "", false
	}
}

// cellOf inverse-maps a dispatched client-space cell centre to grid
// coordinates; off-centre coordinates truncate toward the containing cell
// (the mapping is diagnostic — receipts only, never model state).
func cellOf(xPx, yPx int32) (int32, int32) {
	x := (xPx - gridOriginXPx - cellSizePx/2) / cellSizePx
	y := (yPx - gridOriginYPx - cellSizePx/2) / cellSizePx
	return x, y
}
