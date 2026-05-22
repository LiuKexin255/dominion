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

	// ErrNoRuntimeAvailable indicates that no runtime can be allocated.
	ErrNoRuntimeAvailable = errors.New("no runtime available")
)
