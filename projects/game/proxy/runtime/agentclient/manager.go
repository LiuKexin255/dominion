package agentclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	"dominion/common/gopkg/bootstrap"
	"dominion/common/gopkg/solver"

	"google.golang.org/grpc"
)

// DefaultRefreshInterval is the default interval between agent client refreshes.
const DefaultRefreshInterval = 30 * time.Second

// Manager manages cached agent gRPC clients with periodic refresh.
type Manager interface {
	// Get returns the cached client for the given owner index.
	Get(ctx context.Context, ownerIndex int) (Client, error)
	// List returns all cached client references.
	List(ctx context.Context) ([]ClientRef, error)
	// Close shuts down the background refresh goroutine and closes all connections.
	Close() error
}

// clientEntry holds a cached agent client with its connection and owner metadata.
type clientEntry struct {
	conn       *grpc.ClientConn
	client     Client
	ownerIndex int
	ownerName  string
}

// manager implements Manager and bootstrap.Component.
type manager struct {
	resolver        solver.StatefulResolver
	target          *solver.Target
	entries         map[int]*clientEntry
	mu              sync.RWMutex
	refreshInterval time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
	newClient       func(ctx context.Context, instanceIndex int) (Client, error)
}

// NewManager creates a new manager with the given resolver, target, and refresh interval.
func NewManager(ctx context.Context, resolver solver.StatefulResolver, target *solver.Target, refreshInterval time.Duration) *manager {
	if refreshInterval <= 0 {
		refreshInterval = DefaultRefreshInterval
	}
	m := &manager{
		resolver:        resolver,
		target:          target,
		entries:         make(map[int]*clientEntry),
		refreshInterval: refreshInterval,
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.newClient = func(ctx context.Context, instanceIndex int) (Client, error) {
		return NewAgentClient(ctx, instanceIndex)
	}
	return m
}

// Name returns the component name.
func (m *manager) Name() string {
	return "agentclient-manager"
}

// Stage returns the lifecycle stage (StageDaemon = 250).
func (m *manager) Stage() bootstrap.Stage {
	return bootstrap.StageDaemon
}

// Start triggers an initial refresh synchronously, then starts the background refresh goroutine.
func (m *manager) Start(ctx context.Context) error {
	if err := m.refresh(); err != nil {
		return fmt.Errorf("agentclient: initial refresh failed: %w", err)
	}
	go m.runRefreshLoop()
	return nil
}

// Stop gracefully shuts down the component.
func (m *manager) Stop(ctx context.Context) error {
	return m.Close()
}

// Get returns the cached client for the given owner index.
func (m *manager) Get(ctx context.Context, ownerIndex int) (Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.entries[ownerIndex]
	if !ok {
		return nil, fmt.Errorf("agentclient: no client for owner index %d", ownerIndex)
	}
	return entry.client, nil
}

// List returns all cached client references.
func (m *manager) List(ctx context.Context) ([]ClientRef, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var refs []ClientRef
	for _, entry := range m.entries {
		refs = append(refs, ClientRef{
			OwnerIndex: entry.ownerIndex,
			Owner:      entry.ownerName,
			Client:     entry.client,
		})
	}
	return refs, nil
}

// Close cancels the background refresh goroutine and closes all cached connections.
func (m *manager) Close() error {
	m.cancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range m.entries {
		if entry.client != nil {
			entry.client.Close()
		}
	}
	m.entries = nil
	return nil
}

// refresh resolves current instances, creates clients for new instances,
// and closes clients for removed instances.
func (m *manager) refresh() error {
	instances, err := m.resolver.Resolve(m.ctx, m.target)
	if err != nil {
		return fmt.Errorf("agentclient: resolve failed: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Build set of current instance indices.
	current := make(map[int]*solver.StatefulInstance, len(instances))
	for _, inst := range instances {
		current[inst.Index] = inst
	}

	// Remove stale entries.
	for index, entry := range m.entries {
		if _, ok := current[index]; !ok {
			if entry.client != nil {
				entry.client.Close()
			}
			delete(m.entries, index)
		}
	}

	// Create new entries.
	for index, inst := range current {
		if _, ok := m.entries[index]; ok {
			continue
		}
		client, err := m.newClient(m.ctx, index)
		if err != nil {
			return fmt.Errorf("agentclient: create client for instance %d: %w", index, err)
		}
		var conn *grpc.ClientConn
		if ac, ok := client.(*AgentClient); ok {
			conn = ac.conn
		}
		m.entries[index] = &clientEntry{
			conn:       conn,
			client:     client,
			ownerIndex: index,
			ownerName:  inst.Hostname,
		}
	}

	return nil
}

// runRefreshLoop runs the periodic refresh loop until the context is cancelled.
func (m *manager) runRefreshLoop() {
	ticker := time.NewTicker(m.refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.refresh()
		}
	}
}
