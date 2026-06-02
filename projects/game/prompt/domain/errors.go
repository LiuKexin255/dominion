package domain

import "errors"

var (
	// ErrNotFound indicates the requested resource does not exist.
	ErrNotFound = errors.New("resource not found")
	// ErrAlreadyExists indicates a resource with the given name already exists.
	ErrAlreadyExists = errors.New("resource already exists")
)
