// Package domain provides the domain models and interfaces for the agent service.
package domain

import "time"

// Status represents the current state of an agent session.
type Status struct {
	// SessionId is the unique identifier of the session.
	SessionId string
	// Status is the current status string (e.g. "initialized", "unknown").
	Status string
	// CreateTime is the timestamp when this status was recorded.
	CreateTime time.Time
}
