// Package domain contains the game gateway domain model.
package domain

import (
	"time"
)

// StreamState describes the media stream status of a session runtime.
type StreamState int

const (
	// StreamStateUnspecified is the zero value.
	StreamStateUnspecified StreamState = 0
	// StreamStateActive indicates the agent is actively streaming media.
	StreamStateActive StreamState = 1
	// StreamStatePaused indicates the media stream is paused.
	StreamStatePaused StreamState = 2
	// StreamStateUnavailable indicates no media stream is available.
	StreamStateUnavailable StreamState = 3
)

// SessionRuntime holds the in-memory state of a game session running on a
// gateway instance. It tracks connections, stream state, and the current
// inflight control operation.
//
// Design constraints:
//   - At most one AgentConnection.
//   - Multiple WebConnections allowed.
//   - At most one InflightOperation at a time.
type SessionRuntime struct {
	// SessionID identifies the game session.
	SessionID string
	// GatewayID identifies the gateway instance hosting this runtime.
	GatewayID string
	// ReconnectGeneration increments on each gateway reassignment.
	ReconnectGeneration int64
	// AgentConn is the current agent connection, or nil if none.
	AgentConn *AgentConnection
	// WebConns holds all active web viewer connections.
	WebConns []*WebConnection
	// StreamState indicates the current media stream status.
	StreamState StreamState
	// LatestSnapshot is the most recent snapshot captured from the media stream.
	LatestSnapshot *SnapshotRef
	// InflightOp is the currently executing control operation, or nil.
	InflightOp *InflightOperation
	// LastMediaTime records when the last media segment was received.
	LastMediaTime time.Time
	// LastSnapshotTime records when the last snapshot was captured.
	LastSnapshotTime time.Time
	// LastError holds a human-readable description of the most recent error.
	LastError string
}

// AgentConnection represents a single Windows agent connected via WebSocket.
type AgentConnection struct {
	// ConnID uniquely identifies the WebSocket connection.
	ConnID string
}

// WebConnection represents a single web viewer connected via WebSocket.
type WebConnection struct {
	// ConnID uniquely identifies the WebSocket connection.
	ConnID string
}
