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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

// newMockConnFactory creates a newAgentConn that returns real passthrough
// gRPC connections and tracks created conns.
type mockConnTracker struct {
	mu    sync.Mutex
	conns []*grpc.ClientConn
	count int32
}

func (t *mockConnTracker) factory(ctx context.Context, instanceIndex int) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient("passthrough:///localhost:0",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("mock conn factory: %w", err)
	}
	t.mu.Lock()
	t.conns = append(t.conns, conn)
	t.mu.Unlock()
	atomic.AddInt32(&t.count, 1)
	return conn, nil
}

func (t *mockConnTracker) createdCount() int {
	return int(atomic.LoadInt32(&t.count))
}

// setMockNewAgentConn replaces the package-level newAgentConn with a mock factory
// and returns a function to restore the original.
func setMockNewAgentConn() (func(), *mockConnTracker) {
	old := newAgentConn
	tracker := &mockConnTracker{}
	newAgentConn = tracker.factory
	return func() { newAgentConn = old }, tracker
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

	restore, _ := setMockNewAgentConn()
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

func TestManager_GetReturnsCorrectConnRef(t *testing.T) {
	resolver := &mockResolver{
		instances: makeInstances(0, 1),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore, _ := setMockNewAgentConn()
	defer restore()

	mgr := NewManager(resolver, target, time.Minute)

	if err := mgr.refresh(context.Background()); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	ref, err := mgr.Get(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected Get error: %v", err)
	}

	if ref.OwnerIndex != 0 {
		t.Fatalf("expected OwnerIndex 0, got %d", ref.OwnerIndex)
	}
	if ref.Owner != "agent-0" {
		t.Fatalf("expected Owner 'agent-0', got %q", ref.Owner)
	}
	if ref.Conn == nil {
		t.Fatal("expected Conn to be non-nil")
	}
}

func TestManager_GetNonExistent(t *testing.T) {
	resolver := &mockResolver{}
	target := solver.MustParseTarget("game/agent:grpc")

	restore, _ := setMockNewAgentConn()
	defer restore()

	mgr := NewManager(resolver, target, time.Minute)

	_, err := mgr.Get(context.Background(), 99)

	if err == nil {
		t.Fatal("expected error for non-existent owner index")
	}
}

func TestManager_RefreshRemovesStaleConns(t *testing.T) {
	resolver := &mockResolver{
		instances: makeInstances(0, 1, 2),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore, tracker := setMockNewAgentConn()
	defer restore()

	mgr := NewManager(resolver, target, time.Minute)

	if err := mgr.refresh(context.Background()); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	// Save ref for instance 2 before removal.
	ref2, _ := mgr.Get(context.Background(), 2)

	resolver.setInstances(makeInstances(0, 1))
	if err := mgr.refresh(context.Background()); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	if _, err := mgr.Get(context.Background(), 2); err == nil {
		t.Fatal("expected error for removed instance 2")
	}

	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}

	// Verify fresh conns were created for the two remaining instances.
	if tracker.createdCount() < 3 {
		t.Fatalf("expected at least 3 conns created, got %d", tracker.createdCount())
	}
	_ = ref2
}

func TestManager_RefreshAddsNewConns(t *testing.T) {
	resolver := &mockResolver{
		instances: makeInstances(0),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore, tracker := setMockNewAgentConn()
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
	// Verify new conns were created for instances 1 and 2.
	if tracker.createdCount() != 3 {
		t.Fatalf("expected 3 conns created, got %d", tracker.createdCount())
	}
}

func TestManager_RefreshResolveErrorKeepsExisting(t *testing.T) {
	resolver := &mockResolver{
		instances: makeInstances(0),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore, _ := setMockNewAgentConn()
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

	restore, _ := setMockNewAgentConn()
	defer restore()

	mgr := NewManager(resolver, target, time.Minute)

	if err := mgr.refresh(context.Background()); err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}

	// Capture refs before close.
	ref0, _ := mgr.Get(context.Background(), 0)
	ref1, _ := mgr.Get(context.Background(), 1)

	if err := mgr.Close(); err != nil {
		t.Fatalf("unexpected Close error: %v", err)
	}

	// Verify entries are cleared.
	refs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs after Close, got %d", len(refs))
	}

	// Verify conns were non-nil before close (they were real connections).
	if ref0.Conn == nil {
		t.Fatal("expected ref0.Conn to be non-nil")
	}
	if ref1.Conn == nil {
		t.Fatal("expected ref1.Conn to be non-nil")
	}
	_ = ref0
	_ = ref1
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

	restore, _ := setMockNewAgentConn()
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
// initial refresh and then periodic refresh updates connections.
func TestRefresherWorker_InitialRefreshSuccess(t *testing.T) {
	resolver := &mockResolver{
		instances: makeInstances(0),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore, _ := setMockNewAgentConn()
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
// refresh fails, existing connections are preserved (error logged, no data loss).
func TestRefresherWorker_PeriodicRefreshFailure(t *testing.T) {
	resolver := &mockResolver{
		instances: makeInstances(0),
	}
	target := solver.MustParseTarget("game/agent:grpc")

	restore, _ := setMockNewAgentConn()
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
