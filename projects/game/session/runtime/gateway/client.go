package gateway

import (
	"context"
	"time"
)

// InitResult holds the response from initializing a game runtime on a gateway.
type InitResult struct {
	// OwnerGatewayID is the gateway that owns the session runtime.
	OwnerGatewayID string
	// OwnerEpoch is the ownership epoch assigned by the gateway.
	OwnerEpoch int64
	// Token is the gateway-issued token for connecting to the game.
	Token string
	// ExpiresAt is when the token expires.
	ExpiresAt time.Time
}

// RefreshResult holds the response from refreshing a game runtime on a gateway.
type RefreshResult struct {
	// OwnerGatewayID is the gateway that now owns the session runtime.
	OwnerGatewayID string
	// OwnerEpoch is the ownership epoch assigned by the gateway.
	OwnerEpoch int64
	// ReconnectGeneration is the updated reconnect generation.
	ReconnectGeneration int64
	// Token is the new gateway-issued token for connecting to the game.
	Token string
	// ExpiresAt is when the new token expires.
	ExpiresAt time.Time
}

// GatewayClient calls gateway internal endpoints to manage game runtimes.
type GatewayClient interface {
	// InitGameRuntime creates a game runtime on a gateway for the given session.
	InitGameRuntime(ctx context.Context, sessionID string, reconnectGeneration int64) (*InitResult, error)

	// RefreshGameRuntime refreshes a game runtime, typically during reconnect.
	RefreshGameRuntime(ctx context.Context, sessionID string, oldToken string) (*RefreshResult, error)
}
