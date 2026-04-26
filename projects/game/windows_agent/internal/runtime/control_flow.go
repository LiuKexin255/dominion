package runtime

import (
	"context"
	"fmt"

	gw "dominion/projects/game/gateway"
	"dominion/projects/game/gateway/domain"
	"dominion/projects/game/windows_agent/internal/input"
)

// handleControlRequest acknowledges, executes, and reports one gateway control request.
func (r *Runtime) handleControlRequest(req *gw.GameControlRequest) error {
	if req == nil {
		return fmt.Errorf("control request is nil")
	}
	if r.inputMgr == nil {
		return fmt.Errorf("input manager is not configured")
	}
	session := r.currentSession()
	if session == nil {
		return fmt.Errorf("session is not initialized")
	}
	operationID := req.GetOperationId()
	if err := r.transport.SendControlAck(r.ctx, session.ID, operationID); err != nil {
		return err
	}

	r.mu.RLock()
	boundWindow := r.boundWindow
	r.mu.RUnlock()
	if boundWindow == nil {
		return r.transport.SendControlResult(r.ctx, session.ID, operationID, gw.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_FAILED)
	}

	cmd, err := input.ConvertControlRequest(toControlPayload(req), boundWindow.HWND)
	if err == nil {
		_, err = r.inputMgr.ExecuteCommand(context.Background(), cmd)
	}
	status := gw.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED
	if err != nil {
		status = gw.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_FAILED
	}
	if sendErr := r.transport.SendControlResult(r.ctx, session.ID, operationID, status); sendErr != nil {
		return sendErr
	}
	return err
}

func toControlPayload(req *gw.GameControlRequest) *domain.ControlRequestPayload {
	mouse := req.GetMouse()
	return &domain.ControlRequestPayload{
		RequestID:     req.GetOperationId(),
		Kind:          toDomainOperationKind(req.GetKind()),
		Button:        mouseButtonToString(mouse.GetButton()),
		X:             mouse.GetX(),
		Y:             mouse.GetY(),
		FromX:         mouse.GetFromX(),
		FromY:         mouse.GetFromY(),
		ToX:           mouse.GetToX(),
		ToY:           mouse.GetToY(),
		DurationMs:    mouse.GetDurationMs(),
		FlashSnapshot: req.GetFlashSnapshot(),
	}
}

func toDomainOperationKind(kind gw.GameControlOperationKind) domain.OperationKind {
	switch kind {
	case gw.GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK:
		return domain.OperationKindMouseClick
	case gw.GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DOUBLE_CLICK:
		return domain.OperationKindMouseDoubleClick
	case gw.GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DRAG:
		return domain.OperationKindMouseDrag
	case gw.GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOVER:
		return domain.OperationKindMouseHover
	case gw.GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOLD:
		return domain.OperationKindMouseHold
	default:
		return ""
	}
}

func mouseButtonToString(button gw.GameMouseButton) string {
	switch button {
	case gw.GameMouseButton_GAME_MOUSE_BUTTON_LEFT:
		return string(input.ButtonLeft)
	case gw.GameMouseButton_GAME_MOUSE_BUTTON_RIGHT:
		return string(input.ButtonRight)
	case gw.GameMouseButton_GAME_MOUSE_BUTTON_MIDDLE:
		return string(input.ButtonMiddle)
	default:
		return ""
	}
}
