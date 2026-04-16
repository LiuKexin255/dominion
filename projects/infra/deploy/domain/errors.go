// Package domain contains the deploy service domain model.
package domain

import "errors"

var (
	// ErrNotFound indicates that the requested environment does not exist.
	ErrNotFound = errors.New("environment does not exist")

	// ErrAlreadyExists indicates that the environment already exists.
	ErrAlreadyExists = errors.New("environment already exists")

	// ErrInvalidState indicates that the requested state transition is invalid.
	ErrInvalidState = errors.New("invalid state transition")

	// ErrInvalidName indicates that the resource name is invalid.
	ErrInvalidName = errors.New("invalid resource name")

	// ErrInvalidSpec indicates that the deployment spec is invalid.
	ErrInvalidSpec = errors.New("invalid deployment spec")

	// ErrInvalidType indicates that the environment type is invalid.
	ErrInvalidType = errors.New("invalid environment type")
)
