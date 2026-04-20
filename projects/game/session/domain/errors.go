// Package domain contains the game session domain model.
package domain

import "errors"

var (
	// ErrNotFound indicates that the requested session does not exist.
	ErrNotFound = errors.New("session does not exist")

	// ErrAlreadyExists indicates that the session already exists.
	ErrAlreadyExists = errors.New("session already exists")

	// ErrInvalidState indicates that the requested state transition is invalid.
	ErrInvalidState = errors.New("invalid state transition")

	// ErrInvalidType indicates that the session type is invalid.
	ErrInvalidType = errors.New("invalid session type")

	// ErrSessionEnded indicates that the session has already ended.
	ErrSessionEnded = errors.New("session already ended")

	// ErrNoGatewayAvailable indicates that no gateway can be allocated.
	ErrNoGatewayAvailable = errors.New("no gateway available")
)
