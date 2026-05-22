// Package sessionmanager provides the session runtime manager for the game runtime.
//
// The Manager holds in-memory SessionRuntime state for all active sessions on
// a runtime instance. It coordinates agent/web connections and inflight
// operations with thread-safe access via sync.RWMutex.
package sessionmanager

import (
	"context"
	"sync"
	"time"

	"dominion/common/gopkg/bootstrap"
	"dominion/projects/game/runtime/domain"
)

// ManagerOption is a functional option for configuring Manager.
type ManagerOption func(*Manager)

// Manager manages the runtime state of active game sessions on a runtime
// instance. All methods are safe for concurrent use.
type Manager struct {
	mu        sync.RWMutex
	runtimeID string
	sessions  map[string]*domain.SessionRuntime
	idleTTL   time.Duration
}

// WithIdleTTL sets the idle TTL for sessions managed by this Manager.
func WithIdleTTL(ttl time.Duration) ManagerOption {
	return func(m *Manager) {
		m.idleTTL = ttl
	}
}

// NewManager creates a Manager with an empty session map for the given runtime.
// Optional ManagerOption arguments configure additional behaviour.
func NewManager(runtimeID string, opts ...ManagerOption) *Manager {
	m := &Manager{
		runtimeID: runtimeID,
		sessions:  map[string]*domain.SessionRuntime{},
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// GetOrCreateRuntime returns the existing SessionRuntime for sessionID, or
// creates a new one with the given sessionID and the Manager's runtimeID.
func (m *Manager) GetOrCreateRuntime(sessionID string) *domain.SessionRuntime {
	m.mu.Lock()
	defer m.mu.Unlock()

	if rt, ok := m.sessions[sessionID]; ok {
		return rt
	}

	rt := &domain.SessionRuntime{
		SessionID:       sessionID,
		RuntimeID:       m.runtimeID,
		LastTrafficTime: time.Now(),
	}
	m.sessions[sessionID] = rt
	return rt
}

// TouchSession updates the LastTrafficTime for the given session to the
// current time. Returns ErrSessionNotFound if the session does not exist.
func (m *Manager) TouchSession(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rt, ok := m.sessions[sessionID]
	if !ok {
		return domain.ErrSessionNotFound
	}

	rt.LastTrafficTime = time.Now()
	return nil
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

// RemoveRuntime removes the session with the given sessionID from the manager.
// It is safe for concurrent use and is a no-op if the session does not exist.
func (m *Manager) RemoveRuntime(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// Len returns the number of active sessions.
func (m *Manager) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// StartCleanup returns a bootstrap.WorkerBuilder that creates a worker to
// periodically remove sessions idle beyond the configured idleTTL. If idleTTL
// is 0, the worker is a no-op.
func (m *Manager) StartCleanup() bootstrap.WorkerBuilder {
	return bootstrap.WorkerBuilderFunc(func(_ context.Context) (bootstrap.Worker, error) {
		return &cleanupWorker{
			manager: m,
			idleTTL: m.idleTTL,
		}, nil
	})
}

// cleanupWorker implements bootstrap.Worker for periodic idle session cleanup.
type cleanupWorker struct {
	manager *Manager
	idleTTL time.Duration
	cancel  context.CancelFunc
}

// Start begins the periodic cleanup loop. It blocks until the context is
// cancelled. If idleTTL is 0, it returns immediately (no-op).
func (w *cleanupWorker) Start(ctx context.Context) error {
	if w.idleTTL <= 0 {
		return nil
	}

	ctx, w.cancel = context.WithCancel(ctx)
	defer w.cancel()

	interval := w.idleTTL / 2
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.manager.removeIdleSessions(w.idleTTL)
		case <-ctx.Done():
			return nil
		}
	}
}

// Stop requests the cleanup worker to exit. Safe to call multiple times.
func (w *cleanupWorker) Stop(_ context.Context) error {
	if w.cancel != nil {
		w.cancel()
	}
	return nil
}

// removeIdleSessions removes all sessions that have been idle longer than ttl.
func (m *Manager) removeIdleSessions(ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for id, rt := range m.sessions {
		if now.Sub(rt.LastTrafficTime) > ttl {
			delete(m.sessions, id)
		}
	}
}
