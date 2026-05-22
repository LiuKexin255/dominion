package runtimeclient

import (
	"context"
	"time"
)

// InitResult holds the response from initializing a game runtime on a runtime instance.
type InitResult struct {
	OwnerRuntimeID string
	OwnerEpoch     int64
	Token          string
	ExpiresAt      time.Time
}

// RefreshResult holds the response from refreshing a game runtime on a runtime instance.
type RefreshResult struct {
	OwnerRuntimeID      string
	OwnerEpoch          int64
	ReconnectGeneration int64
	Token               string
	ExpiresAt           time.Time
}

// RuntimeClient calls runtime internal endpoints to manage game runtimes.
type RuntimeClient interface {
	InitGameRuntime(ctx context.Context, sessionID string, reconnectGeneration int64) (*InitResult, error)

	RefreshGameRuntime(ctx context.Context, sessionID string, oldToken string) (*RefreshResult, error)
}
