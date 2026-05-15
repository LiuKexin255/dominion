package gateway

import (
	"fmt"
)

const (
	// MaxHoldDurationMs is the maximum allowed hold duration in milliseconds.
	MaxHoldDurationMs = 30000
	// ErrCodeProtocolError is the error code for protocol errors.
	ErrCodeProtocolError = "protocol_error"
)

// ValidateWebSocketEnvelope checks that the envelope has the required fields:
// session_id non-empty, message_id non-empty, and payload oneof set.
func ValidateWebSocketEnvelope(env *GameWebSocketEnvelope) error {
	if env.GetSessionId() == "" {
		return fmt.Errorf("session_id is empty")
	}
	if env.GetMessageId() == "" {
		return fmt.Errorf("message_id is empty")
	}
	if env.Payload == nil {
		return fmt.Errorf("payload not set")
	}
	return nil
}

// ValidateHello checks that the hello payload exists and role is not UNSPECIFIED.
func ValidateHello(env *GameWebSocketEnvelope) error {
	hello := env.GetHello()
	if hello == nil {
		return fmt.Errorf("hello payload not set")
	}
	if hello.GetRole() == GameClientRole_GAME_CLIENT_ROLE_UNSPECIFIED {
		return fmt.Errorf("role must not be UNSPECIFIED")
	}
	return nil
}

// ValidateRolePayload checks that the message payload direction matches the
// client role.  agent can send: media_init, media_segment, control_ack,
// control_result, pong, error.  web can send: control_request, ping.
func ValidateRolePayload(role GameClientRole, env *GameWebSocketEnvelope) error {
	switch role {
	case GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT:
		switch env.Payload.(type) {
		case *GameWebSocketEnvelope_MediaInit,
			*GameWebSocketEnvelope_MediaSegment,
			*GameWebSocketEnvelope_ControlAck,
			*GameWebSocketEnvelope_ControlResult,
			*GameWebSocketEnvelope_Pong,
			*GameWebSocketEnvelope_Error:
			return nil
		case *GameWebSocketEnvelope_ControlRequest:
			return fmt.Errorf("agent cannot send control_request")
		default:
			return fmt.Errorf("agent cannot send %T", env.Payload)
		}
	case GameClientRole_GAME_CLIENT_ROLE_WEB:
		switch env.Payload.(type) {
		case *GameWebSocketEnvelope_ControlRequest,
			*GameWebSocketEnvelope_Ping:
			return nil
		case *GameWebSocketEnvelope_MediaSegment:
			return fmt.Errorf("web cannot send media_segment")
		default:
			return fmt.Errorf("web cannot send %T", env.Payload)
		}
	default:
		return fmt.Errorf("unsupported client role: %v", role)
	}
}

// ValidateControlRequest validates a control request message: operation_id
// non-empty, action oneof set, and action-specific field constraints.
func ValidateControlRequest(req *GameControlRequest) error {
	if req.GetOperationId() == "" {
		return fmt.Errorf("operation_id is empty")
	}

	switch action := req.Action.(type) {
	case *GameControlRequest_MouseClick:
		return validateClickButtonXY(action.MouseClick.GetButton(), action.MouseClick.GetX(), action.MouseClick.GetY())
	case *GameControlRequest_MouseDoubleClick:
		return validateClickButtonXY(action.MouseDoubleClick.GetButton(), action.MouseDoubleClick.GetX(), action.MouseDoubleClick.GetY())
	case *GameControlRequest_MouseDrag:
		return validateDragAction(action.MouseDrag)
	case *GameControlRequest_MouseHover:
		return validateHoverAction(action.MouseHover)
	case *GameControlRequest_MouseHold:
		return validateHoldAction(action.MouseHold)
	default:
		return fmt.Errorf("action not set in control_request")
	}
}

func validateClickButtonXY(button GameMouseButton, x, y int32) error {
	if button == GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED {
		return fmt.Errorf("button must not be UNSPECIFIED")
	}
	if x < 0 {
		return fmt.Errorf("x must be >= 0")
	}
	if y < 0 {
		return fmt.Errorf("y must be >= 0")
	}
	return nil
}

func validateDragAction(drag *GameMouseDrag) error {
	if drag.GetButton() == GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED {
		return fmt.Errorf("button must not be UNSPECIFIED")
	}
	if drag.GetFromX() < 0 {
		return fmt.Errorf("from_x must be >= 0")
	}
	if drag.GetFromY() < 0 {
		return fmt.Errorf("from_y must be >= 0")
	}
	if drag.GetToX() < 0 {
		return fmt.Errorf("to_x must be >= 0")
	}
	if drag.GetToY() < 0 {
		return fmt.Errorf("to_y must be >= 0")
	}
	return nil
}

func validateHoverAction(hover *GameMouseHover) error {
	if hover.GetX() < 0 {
		return fmt.Errorf("x must be >= 0")
	}
	if hover.GetY() < 0 {
		return fmt.Errorf("y must be >= 0")
	}
	return nil
}

func validateHoldAction(hold *GameMouseHold) error {
	if hold.GetButton() == GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED {
		return fmt.Errorf("button must not be UNSPECIFIED")
	}
	if hold.GetX() < 0 {
		return fmt.Errorf("x must be >= 0")
	}
	if hold.GetY() < 0 {
		return fmt.Errorf("y must be >= 0")
	}
	if hold.GetDurationMs() <= 0 {
		return fmt.Errorf("duration_ms must be > 0")
	}
	if hold.GetDurationMs() > MaxHoldDurationMs {
		return fmt.Errorf("duration_ms exceeds maximum %d", MaxHoldDurationMs)
	}
	return nil
}
