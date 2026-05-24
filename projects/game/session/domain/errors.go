package domain

import (
	"errors"
)

var (
	// ErrNotFound indicates the requested session does not exist.
	ErrNotFound = errors.New("session not found")
	// ErrAlreadyExists indicates a session with the given name already exists.
	ErrAlreadyExists = errors.New("session already exists")
)
