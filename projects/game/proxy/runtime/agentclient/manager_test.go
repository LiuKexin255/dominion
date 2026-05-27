package agentclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dominion/common/gopkg/solver"
	game "dominion/projects/game"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// mockResolver implements solver.StatefulResolver for testing.
type mockResolver struct {
	mu        sync.Mutex
	instances []*solver.StatefulInstance
	err       error
}

func (m *mockResolver) Resolve(ctx context.Context, target *solver.Target) ([]*solver.StatefulInstance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return m.instances, nil
}

func (m *mockResolver) setInstances(instances []*solver.StatefulInstance) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.instances = instances
}

func (m *mockResolver) setError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// mockClient implements Client for testing.
type mockClient struct {
	ownerIndex int
	closed     int32
}

func (m *mockClient) CreateAgent(ctx context.Context, req *game.AgentCreateRequest) (*game.AgentStatus, error) {
	return nil, nil
}

func (m *mockClient) DeleteAgent(ctx context.Context, req *game.AgentDeleteRequest) (*emptypb.Empty, error) {
	return nil, nil
}

func (m *mockClient) GetAgentStatus(ctx context.Context, req *game.GetAgentStatusRequest) (*game.AgentStatus, error) {
	return nil, nil
}

func (m *mockClient) Connect(ctx context.Context, opts ...grpc.CallOption) (game.AgentService_ConnectClient, error) {
	return nil, nil
}

func (m *mockClient) Close() error {
	atomic.StoreInt32(&m.closed, 1)
	return nil
}

func (m *mockClient) isClosed() bool {
	return atomic.LoadInt32(&m.closed) == 1
}

// newMockClientFactory creates a newClient function that returns mock clients.
func newMockClientFactory() func(ctx context.Context, instanceIndex int) (Client, error) {
	return func(ctx context.Context, instanceIndex int) (Client, error) {
		return &mockClient{ownerIndex: instanceIndex}, nil
	}
}

func makeInstances(indices ...int) []*solver.StatefulInstance {
	instances := make([]*solver.StatefulInstance, len(indices))
	for i, idx := range indices {
		instances[i] = &solver.StatefulInstance{
			Index:    idx,
			Hostname: fmt.Sprintf("agent-%d", idx),
		}
	}
	return instances
}

func TestManager_ListAfterRefresh(t *testing.T) {
	// given: a manager with a mock resolver returning two instances
	resolver := &mockResolver{
		instances: makeInstances(0, 1),
	}
	target := solver.MustParseTarget("game/agent:grpc")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewManager(ctx, resolver, target, time.Minute)
	mgr.newClient = newMockClientFactory()

	// when: refreshing
	if err := mgr.refresh(); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	// then: List returns both clients
	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
}

func TestManager_GetReturnsCorrectClient(t *testing.T) {
	// given: a manager with two instances
	resolver := &mockResolver{
		instances: makeInstances(0, 1),
	}
	target := solver.MustParseTarget("game/agent:grpc")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewManager(ctx, resolver, target, time.Minute)
	mgr.newClient = newMockClientFactory()

	if err := mgr.refresh(); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	// when: Get for index 0
	client, err := mgr.Get(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected Get error: %v", err)
	}

	// then: it is a mockClient with the correct ownerIndex
	mc, ok := client.(*mockClient)
	if !ok {
		t.Fatalf("expected *mockClient, got %T", client)
	}
	if mc.ownerIndex != 0 {
		t.Fatalf("expected ownerIndex 0, got %d", mc.ownerIndex)
	}
}

func TestManager_GetNonExistent(t *testing.T) {
	// given: a manager with no instances
	resolver := &mockResolver{}
	target := solver.MustParseTarget("game/agent:grpc")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewManager(ctx, resolver, target, time.Minute)
	mgr.newClient = newMockClientFactory()

	// when: querying for a non-existent index
	_, err := mgr.Get(context.Background(), 99)

	// then: should error
	if err == nil {
		t.Fatal("expected error for non-existent owner index")
	}
}

func TestManager_RefreshRemovesStaleClients(t *testing.T) {
	// given: a manager with instances [0, 1, 2]
	resolver := &mockResolver{
		instances: makeInstances(0, 1, 2),
	}
	target := solver.MustParseTarget("game/agent:grpc")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewManager(ctx, resolver, target, time.Minute)
	mgr.newClient = newMockClientFactory()

	if err := mgr.refresh(); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	// capture the mock client for instance 2 before it gets removed
	client2, _ := mgr.Get(context.Background(), 2)
	mc2 := client2.(*mockClient)

	// when: instances change to [0, 1] only
	resolver.setInstances(makeInstances(0, 1))
	if err := mgr.refresh(); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	// then: instance 2 is removed and closed
	_, err := mgr.Get(context.Background(), 2)
	if err == nil {
		t.Fatal("expected error for removed instance 2")
	}
	if !mc2.isClosed() {
		t.Fatal("expected removed client to be closed")
	}

	// and instances 0, 1 still exist
	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
}

func TestManager_RefreshAddsNewClients(t *testing.T) {
	// given: a manager with instance 0 only
	resolver := &mockResolver{
		instances: makeInstances(0),
	}
	target := solver.MustParseTarget("game/agent:grpc")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewManager(ctx, resolver, target, time.Minute)
	mgr.newClient = newMockClientFactory()

	if err := mgr.refresh(); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	// when: instances expand to [0, 1, 2]
	resolver.setInstances(makeInstances(0, 1, 2))
	if err := mgr.refresh(); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	// then: all three exist
	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("expected 3 refs, got %d", len(refs))
	}
}

func TestManager_RefreshResolveErrorKeepsExisting(t *testing.T) {
	// given: a manager with one cached instance
	resolver := &mockResolver{
		instances: makeInstances(0),
	}
	target := solver.MustParseTarget("game/agent:grpc")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewManager(ctx, resolver, target, time.Minute)
	mgr.newClient = newMockClientFactory()

	if err := mgr.refresh(); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	// when: resolver returns an error
	resolver.setError(errors.New("connection failed"))
	err := mgr.refresh()

	// then: error is returned
	if err == nil {
		t.Fatal("expected refresh error")
	}

	// and existing client is still cached
	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
}

func TestManager_CloseCancelsAndClosesAll(t *testing.T) {
	// given: a manager with two instances
	resolver := &mockResolver{
		instances: makeInstances(0, 1),
	}
	target := solver.MustParseTarget("game/agent:grpc")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewManager(ctx, resolver, target, 10*time.Millisecond)
	mgr.newClient = newMockClientFactory()

	if err := mgr.refresh(); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	// capture mock clients
	client0, _ := mgr.Get(context.Background(), 0)
	mc0 := client0.(*mockClient)
	client1, _ := mgr.Get(context.Background(), 1)
	mc1 := client1.(*mockClient)

	// when: Close is called
	if err := mgr.Close(); err != nil {
		t.Fatalf("unexpected Close error: %v", err)
	}

	// then: all clients are closed
	if !mc0.isClosed() {
		t.Fatal("expected client 0 to be closed")
	}
	if !mc1.isClosed() {
		t.Fatal("expected client 1 to be closed")
	}

	// and entries are cleared
	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs after Close, got %d", len(refs))
	}

	// and context is cancelled
	select {
	case <-mgr.ctx.Done():
	default:
		t.Fatal("expected context to be cancelled")
	}
}

func TestManager_StartRunsBackgroundRefresh(t *testing.T) {
	// given: a manager with a fast refresh interval
	resolver := &mockResolver{
		instances: makeInstances(0),
	}
	target := solver.MustParseTarget("game/agent:grpc")
	ctx := context.Background()

	mgr := NewManager(ctx, resolver, target, 20*time.Millisecond)
	mgr.newClient = newMockClientFactory()

	// when: Start is called (triggers initial refresh + background loop)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("unexpected Start error: %v", err)
	}
	defer mgr.Close()

	// then: initial refresh populated clients
	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref after initial refresh, got %d", len(refs))
	}

	// when: instances change (background loop will pick it up)
	resolver.setInstances(makeInstances(0, 1))
	time.Sleep(100 * time.Millisecond)

	// then: background refresh picked up the new instance
	refs, err = mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs after background refresh, got %d", len(refs))
	}
}

func TestManager_BootstrapComponent(t *testing.T) {
	// given: a manager
	resolver := &mockResolver{
		instances: makeInstances(0),
	}
	target := solver.MustParseTarget("game/agent:grpc")
	ctx := context.Background()

	mgr := NewManager(ctx, resolver, target, time.Minute)
	mgr.newClient = newMockClientFactory()

	// then: Name returns "agentclient-manager"
	if mgr.Name() != "agentclient-manager" {
		t.Fatalf("expected Name 'agentclient-manager', got %q", mgr.Name())
	}

	// then: Stage returns StageDaemon (250)
	if mgr.Stage() != 250 {
		t.Fatalf("expected Stage 250, got %d", mgr.Stage())
	}

	// when: Start is called
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("unexpected Start error: %v", err)
	}

	// then: client is populated
	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}

	// when: Stop is called
	if err := mgr.Stop(context.Background()); err != nil {
		t.Fatalf("unexpected Stop error: %v", err)
	}

	// then: entries are cleared
	refs, err = mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs after Stop, got %d", len(refs))
	}
}

func TestManager_ListEmptyReturnsNil(t *testing.T) {
	// given: a manager with no instances
	resolver := &mockResolver{}
	target := solver.MustParseTarget("game/agent:grpc")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewManager(ctx, resolver, target, time.Minute)

	// when: listing
	refs, err := mgr.List(context.Background())

	// then: returns nil (not empty slice), per golang style
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if refs != nil {
		t.Fatalf("expected nil refs, got %v", refs)
	}
}

func TestManager_DefaultRefreshInterval(t *testing.T) {
	target := solver.MustParseTarget("game/agent:grpc")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewManager(ctx, &mockResolver{}, target, 0)
	if mgr.refreshInterval != DefaultRefreshInterval {
		t.Fatalf("expected refresh interval %v, got %v", DefaultRefreshInterval, mgr.refreshInterval)
	}
}
