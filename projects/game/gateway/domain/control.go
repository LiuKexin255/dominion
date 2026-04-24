package domain

import (
	"time"
)

// OperationKind describes the type of a control operation.
type OperationKind string

const (
	// OperationKindMouseClick maps to proto MOUSE_CLICK.
	OperationKindMouseClick OperationKind = "mouse_click"
	// OperationKindMouseDoubleClick maps to proto MOUSE_DOUBLE_CLICK.
	OperationKindMouseDoubleClick OperationKind = "mouse_double_click"
	// OperationKindMouseDrag maps to proto MOUSE_DRAG.
	OperationKindMouseDrag OperationKind = "mouse_drag"
	// OperationKindMouseHover maps to proto MOUSE_HOVER.
	OperationKindMouseHover OperationKind = "mouse_hover"
	// OperationKindMouseHold maps to proto MOUSE_HOLD.
	OperationKindMouseHold OperationKind = "mouse_hold"
)

// InflightOperation tracks a control operation that has been forwarded to the
// agent and is awaiting a result.
type InflightOperation struct {
	// OperationID uniquely identifies the operation.
	OperationID string
	// Kind is the operation type.
	Kind OperationKind
	// FlashSnapshot indicates whether a snapshot should be captured after the
	// operation completes.
	FlashSnapshot bool
	// CreateTime records when the operation was created.
	CreateTime time.Time
	// RequesterConnID is the web connection ID that initiated the operation.
	RequesterConnID string
}

// ControlCompletion carries the result of a control operation that completed
// asynchronously (timeout or agent disconnect). It provides enough context for
// the service layer to route the result and optionally refresh a snapshot.
type ControlCompletion struct {
	SessionID       string
	RequesterConnID string
	Result          ControlResultPayload
	FlashSnapshot   bool
}

// Timeout constants for control operations.
const (
	// TimeoutClick is the timeout for click, double-click, and hover operations.
	TimeoutClick = 1 * time.Second
	// TimeoutDrag is the timeout for drag operations.
	TimeoutDrag = 30 * time.Second
	// MaxHoldDuration is the maximum allowed duration for a mouse hold operation.
	MaxHoldDuration = 30 * time.Second
	// TimeoutAgentNoResponse is the timeout for agent acknowledgment.
	TimeoutAgentNoResponse = 60 * time.Second
	// SnapshotFreshThreshold is the maximum age for a snapshot to be considered fresh.
	SnapshotFreshThreshold = 1 * time.Second
)
