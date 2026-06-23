package agentclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	"dominion/common/gopkg/bootstrap"
	pgrpc "dominion/common/gopkg/grpc"
	grpcsolver "dominion/common/gopkg/grpc/solver"
	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/solver"
	gameconst "dominion/projects/game/pkg/gameconst"

	"google.golang.org/grpc"
)

// DefaultRefreshInterval is the default interval between agent client refreshes.
const DefaultRefreshInterval = 30 * time.Second

// Manager manages cached agent gRPC connections with periodic refresh.
type Manager interface {
	// Get returns the cached connection for the given owner index.
	Get(ctx context.Context, ownerIndex int) (*ConnRef, error)
	// List returns all cached connection references.
	List(ctx context.Context) ([]*ConnRef, error)
	// Close shuts down the background refresh goroutine and closes all connections.
	Close() error
}

// connEntry holds a cached agent connection with its owner metadata.
type connEntry struct {
	conn       *grpc.ClientConn
	ownerIndex int
	ownerName  string
}

// manager implements Manager.
type manager struct {
	resolver        solver.StatefulResolver
	target          *solver.Target
	entries         map[int]*connEntry
	mu              sync.RWMutex
	refreshInterval time.Duration
}

// newAgentConn is a package-level variable for creating gRPC connections.
// Tests can replace it with a mock factory via save/restore.
var newAgentConn = func(ctx context.Context, instanceIndex int) (*grpc.ClientConn, error) {
	uri := grpcsolver.URI(gameconst.AgentTarget, grpcsolver.WithInstance(instanceIndex))
	opts := append(
		pgrpc.ClientDefault(),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(8*1024*1024),
			grpc.MaxCallSendMsgSize(8*1024*1024),
		),
	)
	return grpc.NewClient(uri, opts...)
}

// NewManager creates a new manager with the given resolver, target, and refresh interval.
func NewManager(resolver solver.StatefulResolver, target *solver.Target, refreshInterval time.Duration) *manager {
	if refreshInterval <= 0 {
		refreshInterval = DefaultRefreshInterval
	}
	return &manager{
		resolver:        resolver,
		target:          target,
		entries:         make(map[int]*connEntry),
		refreshInterval: refreshInterval,
	}
}

// Get returns the cached connection for the given owner index.
func (m *manager) Get(ctx context.Context, ownerIndex int) (*ConnRef, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.entries[ownerIndex]
	if !ok {
		return nil, fmt.Errorf("agentclient: no connection for owner index %d", ownerIndex)
	}
	return &ConnRef{
		OwnerIndex: entry.ownerIndex,
		Owner:      entry.ownerName,
		Conn:       entry.conn,
	}, nil
}

// List returns all cached connection references.
func (m *manager) List(ctx context.Context) ([]*ConnRef, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var refs []*ConnRef
	for _, entry := range m.entries {
		refs = append(refs, &ConnRef{
			OwnerIndex: entry.ownerIndex,
			Owner:      entry.ownerName,
			Conn:       entry.conn,
		})
	}
	return refs, nil
}

// Close closes all cached connections and clears the entry map.
func (m *manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range m.entries {
		if entry.conn != nil {
			entry.conn.Close()
		}
	}
	m.entries = nil
	return nil
}

// refresh resolves current instances, creates connections for new instances,
// and closes connections for removed instances.
func (m *manager) refresh(ctx context.Context) error {
	instances, err := m.resolver.Resolve(ctx, m.target)
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
			if entry.conn != nil {
				entry.conn.Close()
			}
			delete(m.entries, index)
		}
	}

	// Create new entries.
	for index, inst := range current {
		if _, ok := m.entries[index]; ok {
			continue
		}
		conn, err := newAgentConn(ctx, index)
		if err != nil {
			return fmt.Errorf("agentclient: create connection for instance %d: %w", index, err)
		}
		m.entries[index] = &connEntry{
			conn:       conn,
			ownerIndex: index,
			ownerName:  inst.Hostname,
		}
	}

	return nil
}

// RefresherBuilder builds a refresherWorker for the daemon.
type RefresherBuilder struct {
	Manager  *manager
	Interval time.Duration
}

// Build creates a new refresherWorker.
func (b *RefresherBuilder) Build(ctx context.Context) (bootstrap.Worker, error) {
	return &refresherWorker{
		manager:  b.Manager,
		interval: b.Interval,
	}, nil
}

// refresherWorker implements bootstrap.Worker with a blocking Start.
type refresherWorker struct {
	manager  *manager
	interval time.Duration
}

// Start runs the initial refresh synchronously. Initial refresh failure
// returns an error, which causes the Daemon to restart with exponential
// backoff. After initial success, a periodic refresh loop runs until the
// context is cancelled. Periodic refresh failures are logged but do not
// cause a restart -- old connections are preserved.
func (w *refresherWorker) Start(ctx context.Context) error {
	// Initial refresh -- failure causes Daemon restart with backoff.
	if err := w.manager.refresh(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Periodic refresh -- failure logs error, keeps existing connections.
			if err := w.manager.refresh(ctx); err != nil {
				logs.Warn(ctx, "periodic agent connection refresh failed, keeping existing connections", event.Err(err))
			}
		}
	}
}

// Stop calls manager.Close to clean up all cached connections.
func (w *refresherWorker) Stop(ctx context.Context) error {
	return w.manager.Close()
}

// NewDaemon creates a bootstrap.Component (Daemon) that manages the agent connection
// refresh loop with automatic restart on failure.
func NewDaemon(m Manager, interval time.Duration) bootstrap.Component {
	mgr, ok := m.(*manager)
	if !ok {
		panic("NewDaemon: Manager must be *manager")
	}
	builder := &RefresherBuilder{
		Manager:  mgr,
		Interval: interval,
	}
	return bootstrap.Daemon("agentclient-manager",
		bootstrap.WorkerBuilderFunc(func(ctx context.Context) (bootstrap.Worker, error) {
			return builder.Build(ctx)
		}),
	)
}
