// Package domain contains the game runtime domain model.
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
// runtime instance. It tracks connections, stream state, and the current
// inflight control operation.
//
// Design constraints:
//   - At most one AgentConnection.
//   - Multiple WebConnections allowed.
//   - At most one InflightOperation at a time.
type SessionRuntime struct {
	// SessionID identifies the game session.
	SessionID string
	// RuntimeID identifies the runtime instance hosting this session.
	RuntimeID string
	// ReconnectGeneration increments on each runtime reassignment.
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
	// OwnerRuntimeID identifies the runtime that owns this session's token.
	OwnerRuntimeID string
	// OwnerEpoch is the epoch of the owning runtime at token issue time.
	OwnerEpoch int64
	// LastTrafficTime records the last time any traffic was observed for this
	// session (used for idle TTL tracking).
	LastTrafficTime time.Time
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
