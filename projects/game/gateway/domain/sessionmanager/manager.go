// Package sessionmanager provides the session runtime manager for the game gateway.
//
// The Manager holds in-memory SessionRuntime state for all active sessions on
// a gateway instance. It coordinates agent/web connections and inflight
// operations with thread-safe access via sync.RWMutex.
package sessionmanager

import (
	"sync"

	"dominion/projects/game/gateway/domain"
)

// Manager manages the runtime state of active game sessions on a gateway
// instance. All methods are safe for concurrent use.
type Manager struct {
	mu        sync.RWMutex
	gatewayID string
	sessions  map[string]*domain.SessionRuntime
}

// NewManager creates a Manager with an empty session map for the given gateway.
func NewManager(gatewayID string) *Manager {
	return &Manager{
		gatewayID: gatewayID,
		sessions:  map[string]*domain.SessionRuntime{},
	}
}

// GetOrCreateRuntime returns the existing SessionRuntime for sessionID, or
// creates a new one with the given sessionID and the Manager's gatewayID.
func (m *Manager) GetOrCreateRuntime(sessionID string) *domain.SessionRuntime {
	m.mu.Lock()
	defer m.mu.Unlock()

	if rt, ok := m.sessions[sessionID]; ok {
		return rt
	}

	rt := &domain.SessionRuntime{
		SessionID: sessionID,
		GatewayID: m.gatewayID,
	}
	m.sessions[sessionID] = rt
	return rt
}

// GetRuntime returns the SessionRuntime for sessionID, or nil if not found.
func (m *Manager) GetRuntime(sessionID string) *domain.SessionRuntime {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.sessions[sessionID]
}

// RegisterAgent sets the agent connection for a session. Returns
// ErrAgentAlreadyConnected if an agent is already registered.
func (m *Manager) RegisterAgent(sessionID string, conn *domain.AgentConnection) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rt, ok := m.sessions[sessionID]
	if !ok {
		return domain.ErrSessionNotFound
	}

	if rt.AgentConn != nil {
		return domain.ErrAgentAlreadyConnected
	}

	rt.AgentConn = conn
	return nil
}

// UnregisterAgent clears the agent connection for a session. It also clears
// any inflight operation and sets LastError to "agent disconnected".
func (m *Manager) UnregisterAgent(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rt, ok := m.sessions[sessionID]
	if !ok {
		return domain.ErrSessionNotFound
	}

	rt.AgentConn = nil
	rt.InflightOp = nil
	rt.LastError = "agent disconnected"
	return nil
}

// AddWebConn appends a web viewer connection to the session's connection list.
func (m *Manager) AddWebConn(sessionID string, conn *domain.WebConnection) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rt, ok := m.sessions[sessionID]
	if !ok {
		return domain.ErrSessionNotFound
	}

	rt.WebConns = append(rt.WebConns, conn)
	return nil
}

// RemoveWebConn removes the web connection matching connID from the session.
// If no matching connection is found, it does nothing.
func (m *Manager) RemoveWebConn(sessionID string, connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rt, ok := m.sessions[sessionID]
	if !ok {
		return
	}

	conns := rt.WebConns
	for i, c := range conns {
		if c.ConnID == connID {
			// Remove without preserving order.
			conns[i] = conns[len(conns)-1]
			conns[len(conns)-1] = nil
			conns = conns[:len(conns)-1]
			rt.WebConns = conns
			return
		}
	}
}
