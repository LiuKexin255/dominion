// Package domain defines the core types and interfaces for the agent service.
package domain

import "context"

// Runtime defines the interface for agent runtime operations.
// Implementations provide the actual in-memory or persistent behavior.
type Runtime interface {
	// Create creates the agent for the given session and returns the
	// resulting status.
	Create(ctx context.Context, sessionID string) (*Status, error)
	// Delete removes the agent for the given session.
	// It is idempotent: deleting a non-existent session returns nil.
	Delete(ctx context.Context, sessionID string) error
	// Status returns the current status of the agent for the given session.
	Status(ctx context.Context, sessionID string) (*Status, error)
	// ReceiveScreenshot processes a screenshot sent by a client and returns
	// zero or more output frames (text, thinking, operation, warn).
	ReceiveScreenshot(ctx context.Context, sessionID string, input *ScreenshotInput) ([]*Frame, error)
	// CreateWithProfile creates the agent for the given session with a profile
	// and returns the resulting status.
	CreateWithProfile(ctx context.Context, sessionID string, profileName string) (*Status, error)
	// ReceiveOperationResult handles the result of a desktop operation.
	// Returns zero or more frames (e.g. warn frames for validation errors).
	ReceiveOperationResult(ctx context.Context, sessionID string, result *OperationResult) ([]*Frame, error)
}
