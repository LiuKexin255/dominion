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

// setMockNewAgentClient replaces the package-level newAgentClient with a mock factory
// and returns a function to restore the original.
func setMockNewAgentClient() func() {
	old := newAgentClient
	newAgentClient = newMockClientFactory()
	return func() { newAgentClient = old }
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
	resolver := &mockResolver{
		instances: makeInstances(0, 1),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore := setMockNewAgentClient()
	defer restore()

	mgr := NewManager(resolver, target, time.Minute)

	if err := mgr.refresh(context.Background()); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
}

func TestManager_GetReturnsCorrectClient(t *testing.T) {
	resolver := &mockResolver{
		instances: makeInstances(0, 1),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore := setMockNewAgentClient()
	defer restore()

	mgr := NewManager(resolver, target, time.Minute)

	if err := mgr.refresh(context.Background()); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	client, err := mgr.Get(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected Get error: %v", err)
	}

	mc, ok := client.(*mockClient)
	if !ok {
		t.Fatalf("expected *mockClient, got %T", client)
	}
	if mc.ownerIndex != 0 {
		t.Fatalf("expected ownerIndex 0, got %d", mc.ownerIndex)
	}
}

func TestManager_GetNonExistent(t *testing.T) {
	resolver := &mockResolver{}
	target := solver.MustParseTarget("game/agent:grpc")

	restore := setMockNewAgentClient()
	defer restore()

	mgr := NewManager(resolver, target, time.Minute)

	_, err := mgr.Get(context.Background(), 99)

	if err == nil {
		t.Fatal("expected error for non-existent owner index")
	}
}

func TestManager_RefreshRemovesStaleClients(t *testing.T) {
	resolver := &mockResolver{
		instances: makeInstances(0, 1, 2),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore := setMockNewAgentClient()
	defer restore()

	mgr := NewManager(resolver, target, time.Minute)

	if err := mgr.refresh(context.Background()); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	client2, _ := mgr.Get(context.Background(), 2)
	mc2 := client2.(*mockClient)

	resolver.setInstances(makeInstances(0, 1))
	if err := mgr.refresh(context.Background()); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	if _, err := mgr.Get(context.Background(), 2); err == nil {
		t.Fatal("expected error for removed instance 2")
	}
	if !mc2.isClosed() {
		t.Fatal("expected removed client to be closed")
	}

	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
}

func TestManager_RefreshAddsNewClients(t *testing.T) {
	resolver := &mockResolver{
		instances: makeInstances(0),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore := setMockNewAgentClient()
	defer restore()

	mgr := NewManager(resolver, target, time.Minute)

	if err := mgr.refresh(context.Background()); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	resolver.setInstances(makeInstances(0, 1, 2))
	if err := mgr.refresh(context.Background()); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("expected 3 refs, got %d", len(refs))
	}
}

func TestManager_RefreshResolveErrorKeepsExisting(t *testing.T) {
	resolver := &mockResolver{
		instances: makeInstances(0),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore := setMockNewAgentClient()
	defer restore()

	mgr := NewManager(resolver, target, time.Minute)

	if err := mgr.refresh(context.Background()); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	resolver.setError(errors.New("connection failed"))
	err := mgr.refresh(context.Background())

	if err == nil {
		t.Fatal("expected refresh error")
	}

	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
}

func TestManager_CloseClosesAll(t *testing.T) {
	resolver := &mockResolver{
		instances: makeInstances(0, 1),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore := setMockNewAgentClient()
	defer restore()

	mgr := NewManager(resolver, target, time.Minute)

	if err := mgr.refresh(context.Background()); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	client0, _ := mgr.Get(context.Background(), 0)
	mc0 := client0.(*mockClient)
	client1, _ := mgr.Get(context.Background(), 1)
	mc1 := client1.(*mockClient)

	if err := mgr.Close(); err != nil {
		t.Fatalf("unexpected Close error: %v", err)
	}

	if !mc0.isClosed() {
		t.Fatal("expected client 0 to be closed")
	}
	if !mc1.isClosed() {
		t.Fatal("expected client 1 to be closed")
	}

	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs after Close, got %d", len(refs))
	}
}

func TestManager_ListEmptyReturnsNil(t *testing.T) {
	resolver := &mockResolver{}
	target := solver.MustParseTarget("game/agent:grpc")

	mgr := NewManager(resolver, target, time.Minute)

	refs, err := mgr.List(context.Background())

	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if refs != nil {
		t.Fatalf("expected nil refs, got %v", refs)
	}
}

func TestManager_DefaultRefreshInterval(t *testing.T) {
	target := solver.MustParseTarget("game/agent:grpc")

	mgr := NewManager(&mockResolver{}, target, 0)
	if mgr.refreshInterval != DefaultRefreshInterval {
		t.Fatalf("expected refresh interval %v, got %v", DefaultRefreshInterval, mgr.refreshInterval)
	}
}

// TestManager_NewDaemonComponent tests that NewDaemon creates a valid
// bootstrap.Component with correct Name, Stage, Start, and Stop behaviour.
func TestManager_NewDaemonComponent(t *testing.T) {
	resolver := &mockResolver{
		instances: makeInstances(0),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore := setMockNewAgentClient()
	defer restore()

	mgr := NewManager(resolver, target, time.Minute)
	daemon := NewDaemon(mgr, time.Minute)

	if daemon.Name() != "agentclient-manager" {
		t.Fatalf("expected Name 'agentclient-manager', got %q", daemon.Name())
	}

	if daemon.Stage() != 250 {
		t.Fatalf("expected Stage 250, got %d", daemon.Stage())
	}

	ctx := context.Background()
	if err := daemon.Start(ctx); err != nil {
		t.Fatalf("unexpected Start error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}

	if err := daemon.Stop(ctx); err != nil {
		t.Fatalf("unexpected Stop error: %v", err)
	}

	refs, err = mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs after Stop, got %d", len(refs))
	}
}

// TestRefresherWorker_InitialRefreshSuccess tests that worker.Start performs
// initial refresh and then periodic refresh updates clients.
func TestRefresherWorker_InitialRefreshSuccess(t *testing.T) {
	resolver := &mockResolver{
		instances: makeInstances(0),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore := setMockNewAgentClient()
	defer restore()

	mgr := NewManager(resolver, target, time.Minute)
	worker := &refresherWorker{
		manager:  mgr,
		interval: 20 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref after initial refresh, got %d", len(refs))
	}

	resolver.setInstances(makeInstances(0, 1))
	time.Sleep(100 * time.Millisecond)

	refs, err = mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs after periodic refresh, got %d", len(refs))
	}

	cancel()
	select {
	case werr := <-errCh:
		if werr != nil && werr != context.Canceled {
			t.Fatalf("expected context.Canceled or nil, got %v", werr)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worker to stop")
	}
}

// TestRefresherWorker_InitialRefreshFails tests that worker.Start returns
// an error when the initial refresh fails.
func TestRefresherWorker_InitialRefreshFails(t *testing.T) {
	resolver := &mockResolver{}
	resolver.setError(errors.New("connection failed"))
	target := solver.MustParseTarget("game/agent:grpc")

	mgr := NewManager(resolver, target, time.Minute)
	worker := &refresherWorker{
		manager:  mgr,
		interval: time.Minute,
	}

	err := worker.Start(context.Background())

	if err == nil {
		t.Fatal("expected error from initial refresh failure")
	}
}

// TestRefresherWorker_PeriodicRefreshFailure tests that when a periodic
// refresh fails, existing clients are preserved (error logged, no data loss).
func TestRefresherWorker_PeriodicRefreshFailure(t *testing.T) {
	resolver := &mockResolver{
		instances: makeInstances(0),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore := setMockNewAgentClient()
	defer restore()

	mgr := NewManager(resolver, target, time.Minute)
	worker := &refresherWorker{
		manager:  mgr,
		interval: 30 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	resolver.setError(errors.New("transient error"))
	time.Sleep(100 * time.Millisecond)

	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref preserved after periodic refresh failure, got %d", len(refs))
	}

	cancel()
	<-errCh
}
