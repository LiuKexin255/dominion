package domain

import (
	"errors"
)

var (
	// ErrOwnerNotFound indicates the requested agent owner does not exist.
	ErrOwnerNotFound = errors.New("agent owner not found")
	// ErrOwnerAlreadyExists indicates an agent owner for the session already exists.
	ErrOwnerAlreadyExists = errors.New("agent owner already exists")
	// ErrNoAgentInstances indicates there are no available agent instances.
	ErrNoAgentInstances = errors.New("no agent instances available")
)
