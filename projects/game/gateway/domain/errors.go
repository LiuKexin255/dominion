package domain

import "errors"

var (
	// ErrSessionNotFound indicates the requested session runtime does not exist.
	ErrSessionNotFound = errors.New("session not found")
	// ErrAgentAlreadyConnected indicates a second agent attempted to connect.
	ErrAgentAlreadyConnected = errors.New("agent already connected")
	// ErrNoAgent indicates no agent is connected when one is required.
	ErrNoAgent = errors.New("no agent connected")
	// ErrOperationInflight indicates a concurrent control operation was rejected.
	ErrOperationInflight = errors.New("operation already inflight")
	// ErrInvalidMouseAction indicates the mouse action parameters are invalid.
	ErrInvalidMouseAction = errors.New("invalid mouse action")
	// ErrHoldDurationExceeded indicates the requested hold duration exceeds MaxHoldDuration.
	ErrHoldDurationExceeded = errors.New("hold duration exceeds maximum")
)
