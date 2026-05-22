package runtime

import (
	"context"
	"fmt"

	runtimepb "dominion/projects/game/runtime"
	"dominion/projects/game/windows_agent/internal/input"
)

// handleControlRequest validates, acknowledges, executes, and reports one
// gateway control request.
func (r *Runtime) handleControlRequest(req *runtimepb.GameControlRequest) error {
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

	if err := runtimepb.ValidateControlRequest(req); err != nil {
		_ = r.transport.SendControlResult(r.ctx, session.ID, operationID, runtimepb.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_FAILED)
		return err
	}

	if err := r.transport.SendControlAck(r.ctx, session.ID, operationID); err != nil {
		return err
	}

	r.mu.RLock()
	boundWindow := r.boundWindow
	r.mu.RUnlock()
	if boundWindow == nil {
		return r.transport.SendControlResult(r.ctx, session.ID, operationID, runtimepb.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_FAILED)
	}

	cmd, err := commandFromAction(req, boundWindow.HWND)
	if err == nil {
		_, err = r.inputMgr.ExecuteCommand(context.Background(), cmd)
	}
	status := runtimepb.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED
	if err != nil {
		status = runtimepb.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_FAILED
	}
	if sendErr := r.transport.SendControlResult(r.ctx, session.ID, operationID, status); sendErr != nil {
		return sendErr
	}
	return err
}

func commandFromAction(req *runtimepb.GameControlRequest, hwnd uintptr) (input.Command, error) {
	switch action := req.Action.(type) {
	case *runtimepb.GameControlRequest_MouseClick:
		return input.CommandFromMouseClick(action.MouseClick, hwnd)
	case *runtimepb.GameControlRequest_MouseDoubleClick:
		return input.CommandFromMouseDoubleClick(action.MouseDoubleClick, hwnd)
	case *runtimepb.GameControlRequest_MouseDrag:
		return input.CommandFromMouseDrag(action.MouseDrag, hwnd)
	case *runtimepb.GameControlRequest_MouseHover:
		return input.CommandFromMouseHover(action.MouseHover, hwnd)
	case *runtimepb.GameControlRequest_MouseHold:
		return input.CommandFromMouseHold(action.MouseHold, hwnd)
	default:
		return input.Command{}, fmt.Errorf("unsupported action type")
	}
}
