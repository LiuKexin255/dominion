package gateway

import (
	"fmt"
	"strings"

	"dominion/projects/game/gateway/domain"
)

// ActionKindFromProto extracts the domain.OperationKind from the action oneof
// in a GameControlRequest. Returns an error if no action is set.
func ActionKindFromProto(req *GameControlRequest) (domain.OperationKind, error) {
	switch req.GetAction().(type) {
	case *GameControlRequest_MouseClick:
		return domain.OperationKindMouseClick, nil
	case *GameControlRequest_MouseDoubleClick:
		return domain.OperationKindMouseDoubleClick, nil
	case *GameControlRequest_MouseDrag:
		return domain.OperationKindMouseDrag, nil
	case *GameControlRequest_MouseHover:
		return domain.OperationKindMouseHover, nil
	case *GameControlRequest_MouseHold:
		return domain.OperationKindMouseHold, nil
	default:
		return "", fmt.Errorf("no action set in GameControlRequest")
	}
}

// ProtoOperationKind converts a domain.OperationKind to the corresponding proto
// GameControlOperationKind enum value. Returns UNSPECIFIED for unknown kinds.
func ProtoOperationKind(kind domain.OperationKind) GameControlOperationKind {
	switch kind {
	case domain.OperationKindMouseClick:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK
	case domain.OperationKindMouseDoubleClick:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DOUBLE_CLICK
	case domain.OperationKindMouseDrag:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DRAG
	case domain.OperationKindMouseHover:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOVER
	case domain.OperationKindMouseHold:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOLD
	default:
		return GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_UNSPECIFIED
	}
}

// DomainOperationKind converts a proto GameControlOperationKind to the
// corresponding domain.OperationKind. Returns an error for UNSPECIFIED or
// unknown values — no silent fallback.
func DomainOperationKind(kind GameControlOperationKind) (domain.OperationKind, error) {
	switch kind {
	case GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK:
		return domain.OperationKindMouseClick, nil
	case GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DOUBLE_CLICK:
		return domain.OperationKindMouseDoubleClick, nil
	case GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DRAG:
		return domain.OperationKindMouseDrag, nil
	case GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOVER:
		return domain.OperationKindMouseHover, nil
	case GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOLD:
		return domain.OperationKindMouseHold, nil
	default:
		return "", fmt.Errorf("unknown operation kind: %v", kind)
	}
}

// ProtoMouseButton converts a domain button string ("left", "right", "middle")
// to the corresponding proto GameMouseButton enum value. Returns UNSPECIFIED
// for unknown strings.
func ProtoMouseButton(button string) GameMouseButton {
	switch strings.ToLower(button) {
	case "left":
		return GameMouseButton_GAME_MOUSE_BUTTON_LEFT
	case "right":
		return GameMouseButton_GAME_MOUSE_BUTTON_RIGHT
	case "middle":
		return GameMouseButton_GAME_MOUSE_BUTTON_MIDDLE
	default:
		return GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED
	}
}

// DomainMouseButton converts a proto GameMouseButton to the corresponding
// domain button string. Returns an error for UNSPECIFIED or unknown values.
func DomainMouseButton(mb GameMouseButton) (string, error) {
	switch mb {
	case GameMouseButton_GAME_MOUSE_BUTTON_LEFT:
		return "left", nil
	case GameMouseButton_GAME_MOUSE_BUTTON_RIGHT:
		return "right", nil
	case GameMouseButton_GAME_MOUSE_BUTTON_MIDDLE:
		return "middle", nil
	default:
		return "", fmt.Errorf("unknown mouse button: %v", mb)
	}
}
