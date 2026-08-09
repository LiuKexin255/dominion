package solver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/solver"

	"google.golang.org/grpc/balancer"
	grpcresolver "google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

func TestRegister(t *testing.T) {
	registerOnce = sync.Once{}
	originalRegisterResolver := registerResolver
	originalNewResolverBuilder := newResolverBuilder
	originalNewStatefulResolverBuilder := newStatefulResolverBuilder
	t.Cleanup(func() {
		registerResolver = originalRegisterResolver
		newResolverBuilder = originalNewResolverBuilder
		newStatefulResolverBuilder = originalNewStatefulResolverBuilder
	})

	var gotBuilders []grpcresolver.Builder
	registerResolver = func(builder grpcresolver.Builder) {
		gotBuilders = append(gotBuilders, builder)
	}
	newResolverBuilder = func() grpcresolver.Builder {
		return fakeBuilder{scheme: Scheme}
	}
	newStatefulResolverBuilder = func() grpcresolver.Builder {
		return fakeBuilder{scheme: StatefulScheme}
	}

	// when
	Register()
	Register()

	// then
	if len(gotBuilders) != 2 {
		t.Fatalf("Register() call count = %d, want 2", len(gotBuilders))
	}
	if gotBuilders[0].Scheme() != Scheme {
		t.Fatalf("Register() scheme = %q, want %q", gotBuilders[0].Scheme(), Scheme)
	}
	if gotBuilders[1].Scheme() != StatefulScheme {
		t.Fatalf("Register() stateful scheme = %q, want %q", gotBuilders[1].Scheme(), StatefulScheme)
	}
}

func TestStatefulBuilder_Scheme(t *testing.T) {
	builder := NewStatefulBuilder()

	if got := builder.Scheme(); got != StatefulScheme {
		t.Fatalf("Scheme() = %q, want %q", got, StatefulScheme)
	}
}

func TestStatefulBuilder_Build_Success(t *testing.T) {
	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeStatefulResolver{
		results: []statefulResolveResult{{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 0, Hostname: "svc-0", Endpoints: []string{"10.0.0.1:50051"}}, &solver.StatefulInstance{Index: 1, Hostname: "svc-1", Endpoints: []string{"10.0.0.2:50051"}}}}},
	}
	builder := NewStatefulBuilder(WithStatefulResolver(client))
	builder.NewTicker = func(time.Duration) refreshTicker { return ticker }
	builder.RefreshInterval = time.Hour

	got, err := builder.Build(newStatefulResolverTarget("catalog/grpc:50051", 0), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	t.Cleanup(got.Close)

	if len(cc.states()) != 1 {
		t.Fatalf("Build() update count = %d, want 1", len(cc.states()))
	}
	if gotState := cc.states()[0]; !reflect.DeepEqual(addressStrings(gotState.Addresses), []string{"10.0.0.1:50051"}) {
		t.Fatalf("Build() published addresses = %#v, want %#v", addressStrings(gotState.Addresses), []string{"10.0.0.1:50051"})
	}
}

func TestStatefulBuilder_InstanceMissingPublishesEmptyState(t *testing.T) {
	// The requested instance disappearing is a LEGAL state (e.g. mid-rollout):
	// the resolver publishes an EMPTY address list instead of reporting an
	// error — the LB policy fails RPCs fast (TRANSIENT_FAILURE) and the
	// polling loop recovers on the next refresh (grpc-go DNS resolver
	// semantics; deploy incident 2026-08-09).
	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeStatefulResolver{
		results: []statefulResolveResult{
			{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 5, Hostname: "svc-5", Endpoints: []string{"10.0.0.5:50051"}}}},
			{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 0, Hostname: "svc-0", Endpoints: []string{"10.0.0.1:50051"}}, &solver.StatefulInstance{Index: 1, Hostname: "svc-1", Endpoints: []string{"10.0.0.2:50051"}}}},
		},
	}
	builder := NewStatefulBuilder(WithStatefulResolver(client))
	builder.NewTicker = func(time.Duration) refreshTicker { return ticker }
	builder.RefreshInterval = time.Hour

	got, err := builder.Build(newStatefulResolverTarget("catalog/grpc:50051", 5), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	t.Cleanup(got.Close)
	cc.drainUpdateSignals()

	// Instance 5 disappears on the next resolve: an empty state is
	// published, NOT an error.
	got.ResolveNow(grpcresolver.ResolveNowOptions{})
	if !cc.waitForUpdate(time.Second) {
		t.Fatal("expected an empty-state update after the instance disappeared")
	}
	states := cc.states()
	if len(states) != 2 {
		t.Fatalf("update count = %d, want 2 (initial addresses + empty state)", len(states))
	}
	if len(addressStrings(states[1].Addresses)) != 0 {
		t.Fatalf("published addresses = %#v, want empty (missing instance is legal)", addressStrings(states[1].Addresses))
	}
	if len(cc.reported) != 0 {
		t.Fatalf("ReportError() = %v, want none (empty result is not a resolver error)", cc.reported)
	}
}

func TestStatefulBuilder_InstanceWithoutReadyEndpointsPublishesEmptyState(t *testing.T) {
	// An instance present but without ready endpoints (terminating /
	// not-ready pod) is a transient state: publish empty, not an error.
	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeStatefulResolver{
		results: []statefulResolveResult{
			{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 1, Hostname: "svc-1", Endpoints: []string{"10.0.0.1:50051"}}}},
			{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 1, Hostname: "svc-1"}}},
		},
	}
	builder := NewStatefulBuilder(WithStatefulResolver(client))
	builder.NewTicker = func(time.Duration) refreshTicker { return ticker }
	builder.RefreshInterval = time.Hour

	got, err := builder.Build(newStatefulResolverTarget("catalog/grpc:50051", 1), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	t.Cleanup(got.Close)
	cc.drainUpdateSignals()

	got.ResolveNow(grpcresolver.ResolveNowOptions{})
	if !cc.waitForUpdate(time.Second) {
		t.Fatal("expected an empty-state update after the instance lost its endpoints")
	}
	states := cc.states()
	if len(states) != 2 {
		t.Fatalf("update count = %d, want 2 (initial addresses + empty state)", len(states))
	}
	if len(addressStrings(states[1].Addresses)) != 0 {
		t.Fatalf("published addresses = %#v, want empty (no ready endpoints is legal)", addressStrings(states[1].Addresses))
	}
	if len(cc.reported) != 0 {
		t.Fatalf("ReportError() = %v, want none", cc.reported)
	}
}

func TestStatefulBuilder_InitialResolveEmptyStateSelfHeals(t *testing.T) {
	// Regression (deploy incident 2026-08-09): an initial resolve that
	// yields no addresses for the requested instance (e.g. a rollout window
	// where the EndpointSlice is momentarily empty) is a LEGAL state — the
	// adapter publishes an EMPTY address list (not an error), so Build's
	// initial resolve succeeds and the polling loop survives to self-heal on
	// the next refresh once the instance is resolvable again. (In the real
	// grpc-go environment the LB rejects the empty state with
	// `balancer.ErrBadResolverState`; `Resolver.Resolve` tolerates that —
	// see TestResolver_EmptyStateRejectedByLBIsNotAnError.)
	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeStatefulResolver{
		results: []statefulResolveResult{
			// First resolve: instance 0 absent (rollout window) → empty state.
			{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 1, Hostname: "svc-1", Endpoints: []string{"10.0.0.1:50051"}}}},
			// Next refresh: instance 0 is back.
			{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 0, Hostname: "svc-0", Endpoints: []string{"10.0.0.2:50051"}}}},
		},
	}
	builder := NewStatefulBuilder(WithStatefulResolver(client))
	builder.NewTicker = func(time.Duration) refreshTicker { return ticker }
	builder.RefreshInterval = time.Hour

	got, err := builder.Build(newStatefulResolverTarget("catalog/grpc:50051", 0), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error on an empty initial resolve: %v", err)
	}
	t.Cleanup(got.Close)

	// The initial empty result is published as an empty state (RPCs fail
	// fast via the LB policy), not reported as a resolver error.
	states := cc.states()
	if len(states) != 1 {
		t.Fatalf("initial Build() update count = %d, want 1 (empty state)", len(states))
	}
	if len(addressStrings(states[0].Addresses)) != 0 {
		t.Fatalf("initial addresses = %#v, want empty", addressStrings(states[0].Addresses))
	}
	if len(cc.reported) != 0 {
		t.Fatalf("ReportError() = %v, want none (empty result is legal)", cc.reported)
	}
	cc.drainUpdateSignals()

	// The polling loop survived: the next refresh publishes the recovered
	// instance's addresses.
	ticker.Tick()
	if !cc.waitForUpdate(time.Second) {
		t.Fatal("expected resolver to recover on the next refresh")
	}
	states = cc.states()
	gotState := states[len(states)-1]
	if !reflect.DeepEqual(addressStrings(gotState.Addresses), []string{"10.0.0.2:50051"}) {
		t.Fatalf("recovered addresses = %#v, want %#v", addressStrings(gotState.Addresses), []string{"10.0.0.2:50051"})
	}
}

func TestResolver_EmptyStateRejectedByLBIsNotAnError(t *testing.T) {
	// Regression (deploy incident 2026-08-09): in the real grpc-go stack,
	// `cc.UpdateState` with an empty address list returns
	// `balancer.ErrBadResolverState` (pick_first rejects zero addresses so
	// RPCs fail fast with TRANSIENT_FAILURE). Without this tolerance, the
	// stateful adapter's empty result would surface as a resolver failure —
	// the Build path's initial resolve would fail during a rollout window
	// and permanently kill the channel (the resolver never gets to retry).
	// The rejection must be treated as expected: Resolve() succeeds, the
	// polling loop keeps running, and the next refresh self-heals. An
	// UNCHANGED empty state skips the pointless repeated UpdateState
	// round-trip (grpc-go issue #5048: "If it polls and finds no change, it
	// would be fine to not call UpdateState with the data").
	cc := newFakeClientConn()
	cc.emptyStateErr = balancer.ErrBadResolverState
	ticker := newFakeTicker()
	client := &fakeStatefulResolver{
		results: []statefulResolveResult{
			// Resolve 1 (Build): instance 0 absent (rollout window) → empty state → LB rejects.
			{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 1, Hostname: "svc-1", Endpoints: []string{"10.0.0.1:50051"}}}},
			// Resolve 2: still absent → unchanged empty state → UpdateState skipped.
			{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 1, Hostname: "svc-1", Endpoints: []string{"10.0.0.1:50051"}}}},
			// Resolve 3: instance 0 is back.
			{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 0, Hostname: "svc-0", Endpoints: []string{"10.0.0.2:50051"}}}},
		},
	}
	builder := NewStatefulBuilder(WithStatefulResolver(client))
	builder.NewTicker = func(time.Duration) refreshTicker { return ticker }
	builder.RefreshInterval = time.Hour

	got, err := builder.Build(newStatefulResolverTarget("catalog/grpc:50051", 0), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error when the LB rejects the empty state: %v", err)
	}
	t.Cleanup(got.Close)

	if len(cc.reported) != 0 {
		t.Fatalf("ReportError() = %v, want none (LB empty-state rejection is expected)", cc.reported)
	}

	// Resolve 2 (unchanged empty state): sameState hits, UpdateState skipped —
	// the fake records no state and no ReportError is produced.
	ticker.Tick()
	deadline := time.Now().Add(time.Second)
	for client.callCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(cc.states()) != 0 {
		t.Fatalf("unchanged empty state should skip UpdateState, got %d updates", len(cc.states()))
	}
	if len(cc.reported) != 0 {
		t.Fatalf("ReportError() = %v, want none after the unchanged empty state", cc.reported)
	}

	// Resolve 3: the polling loop survived and publishes the recovered
	// instance's addresses.
	ticker.Tick()
	if !cc.waitForUpdate(time.Second) {
		t.Fatal("expected resolver to recover on the next refresh")
	}
	states := cc.states()
	gotState := states[len(states)-1]
	if !reflect.DeepEqual(addressStrings(gotState.Addresses), []string{"10.0.0.2:50051"}) {
		t.Fatalf("recovered addresses = %#v, want %#v", addressStrings(gotState.Addresses), []string{"10.0.0.2:50051"})
	}
}

func TestResolverInitialResolveSuccess(t *testing.T) {
	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeResolverClient{results: []resolveResult{{addresses: []string{"10.0.0.1:50051", "10.0.0.2:50051"}}}}
	builder := NewBuilder(
		WithResolver(client),
		WithNewTicker(func(time.Duration) refreshTicker { return ticker }),
		WithRefreshInterval(time.Hour),
	)

	got, err := builder.Build(newResolverTarget("catalog/grpc:50051"), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	t.Cleanup(got.Close)

	if len(cc.states()) != 1 {
		t.Fatalf("Build() update count = %d, want 1", len(cc.states()))
	}
	if gotState := cc.states()[0]; !reflect.DeepEqual(addressStrings(gotState.Addresses), []string{"10.0.0.1:50051", "10.0.0.2:50051"}) {
		t.Fatalf("Build() published addresses = %#v, want %#v", addressStrings(gotState.Addresses), []string{"10.0.0.1:50051", "10.0.0.2:50051"})
	}
	if scheme := builder.Scheme(); scheme != Scheme {
		t.Fatalf("Scheme() = %q, want %q", scheme, Scheme)
	}
}

func TestResolverUnchangedRefreshSkipsUpdate(t *testing.T) {
	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeResolverClient{results: []resolveResult{{addresses: []string{"10.0.0.1:50051"}}, {addresses: []string{"10.0.0.1:50051"}}}}
	builder := NewBuilder(
		WithResolver(client),
		WithNewTicker(func(time.Duration) refreshTicker { return ticker }),
		WithRefreshInterval(time.Hour),
	)

	got, err := builder.Build(newResolverTarget("catalog/grpc:50051"), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	t.Cleanup(got.Close)

	resolverInstance, ok := got.(*Resolver)
	if !ok {
		t.Fatalf("Build() resolver type = %T, want *Resolver", got)
	}

	if err := resolverInstance.Resolve(); err != nil {
		t.Fatalf("Resolve() unexpected error: %v", err)
	}
	if len(cc.states()) != 1 {
		t.Fatalf("Resolve() update count = %d, want 1", len(cc.states()))
	}
}

func TestResolverChangedRefreshUpdatesState(t *testing.T) {
	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeResolverClient{results: []resolveResult{{addresses: []string{"10.0.0.1:50051"}}, {addresses: []string{"10.0.0.1:50051", "10.0.0.2:50051"}}}}
	builder := NewBuilder(
		WithResolver(client),
		WithNewTicker(func(time.Duration) refreshTicker { return ticker }),
		WithRefreshInterval(time.Hour),
	)

	got, err := builder.Build(newResolverTarget("catalog/grpc:50051"), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	t.Cleanup(got.Close)

	resolverInstance := got.(*Resolver)
	if err := resolverInstance.Resolve(); err != nil {
		t.Fatalf("Resolve() unexpected error: %v", err)
	}

	states := cc.states()
	if len(states) != 2 {
		t.Fatalf("Resolve() update count = %d, want 2", len(states))
	}
	if gotAddresses := addressStrings(states[1].Addresses); !reflect.DeepEqual(gotAddresses, []string{"10.0.0.1:50051", "10.0.0.2:50051"}) {
		t.Fatalf("Resolve() changed addresses = %#v, want %#v", gotAddresses, []string{"10.0.0.1:50051", "10.0.0.2:50051"})
	}
}

func TestResolverRefreshErrorRetainsLastGoodState(t *testing.T) {
	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeResolverClient{results: []resolveResult{{addresses: []string{"10.0.0.1:50051"}}, {err: errors.New("temporary list failure")}, {addresses: []string{"10.0.0.2:50051"}}}}
	builder := NewBuilder(
		WithResolver(client),
		WithNewTicker(func(time.Duration) refreshTicker { return ticker }),
		WithRefreshInterval(time.Hour),
	)

	got, err := builder.Build(newResolverTarget("catalog/grpc:50051"), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	t.Cleanup(got.Close)
	cc.drainUpdateSignals()

	got.ResolveNow(grpcresolver.ResolveNowOptions{})
	if err := cc.waitForError(time.Second); err == nil || !strings.Contains(err.Error(), "temporary list failure") {
		t.Fatalf("ReportError() = %v, want temporary list failure", err)
	}
	if len(cc.states()) != 1 {
		t.Fatalf("after error update count = %d, want 1", len(cc.states()))
	}

	got.ResolveNow(grpcresolver.ResolveNowOptions{})
	if !cc.waitForUpdate(time.Second) {
		t.Fatalf("ResolveNow() did not publish updated state")
	}

	states := cc.states()
	if len(states) != 2 {
		t.Fatalf("final update count = %d, want 2", len(states))
	}
	if gotAddresses := addressStrings(states[1].Addresses); !reflect.DeepEqual(gotAddresses, []string{"10.0.0.2:50051"}) {
		t.Fatalf("final addresses = %#v, want %#v", gotAddresses, []string{"10.0.0.2:50051"})
	}
}

func TestResolveNow(t *testing.T) {
	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeResolverClient{results: []resolveResult{{addresses: []string{"10.0.0.1:50051"}}, {addresses: []string{"10.0.0.2:50051"}}}}
	builder := NewBuilder(
		WithResolver(client),
		WithNewTicker(func(time.Duration) refreshTicker { return ticker }),
		WithRefreshInterval(time.Hour),
	)

	got, err := builder.Build(newResolverTarget("catalog/grpc:50051"), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	t.Cleanup(got.Close)
	cc.drainUpdateSignals()

	got.ResolveNow(grpcresolver.ResolveNowOptions{})
	if !cc.waitForUpdate(time.Second) {
		t.Fatalf("ResolveNow() did not trigger an update")
	}

	states := cc.states()
	if len(states) != 2 {
		t.Fatalf("ResolveNow() update count = %d, want 2", len(states))
	}
	if gotAddresses := addressStrings(states[1].Addresses); !reflect.DeepEqual(gotAddresses, []string{"10.0.0.2:50051"}) {
		t.Fatalf("ResolveNow() addresses = %#v, want %#v", gotAddresses, []string{"10.0.0.2:50051"})
	}
}

func TestClose(t *testing.T) {
	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeResolverClient{results: []resolveResult{{addresses: []string{"10.0.0.1:50051"}}, {addresses: []string{"10.0.0.2:50051"}}}}
	builder := NewBuilder(
		WithResolver(client),
		WithNewTicker(func(time.Duration) refreshTicker { return ticker }),
		WithRefreshInterval(time.Hour),
	)

	got, err := builder.Build(newResolverTarget("catalog/grpc:50051"), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}

	resolverInstance := got.(*Resolver)
	resolverInstance.Close()

	ticker.Tick()
	got.ResolveNow(grpcresolver.ResolveNowOptions{})
	time.Sleep(50 * time.Millisecond)

	if len(cc.states()) != 1 {
		t.Fatalf("Close() update count = %d, want 1", len(cc.states()))
	}
	if client.callCount() != 1 {
		t.Fatalf("Close() resolve call count = %d, want 1", client.callCount())
	}
	if !ticker.stopped() {
		t.Fatalf("Close() did not stop ticker")
	}
}

type resolveResult struct {
	addresses []string
	err       error
}

type fakeResolverClient struct {
	mu      sync.Mutex
	results []resolveResult
	calls   int
}

type statefulResolveResult struct {
	instances []*solver.StatefulInstance
	err       error
}

type fakeStatefulResolver struct {
	mu      sync.Mutex
	results []statefulResolveResult
	calls   int
}

func (c *fakeResolverClient) Resolve(context.Context, *solver.Target) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.results) == 0 {
		c.calls++
		return nil, nil
	}

	index := c.calls
	if index >= len(c.results) {
		index = len(c.results) - 1
	}
	result := c.results[index]
	c.calls++

	if result.err != nil {
		return nil, result.err
	}

	if len(result.addresses) == 0 {
		return nil, nil
	}

	addresses := make([]string, len(result.addresses))
	copy(addresses, result.addresses)
	return addresses, nil
}

func (c *fakeResolverClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (r *fakeStatefulResolver) Resolve(context.Context, *solver.Target) ([]*solver.StatefulInstance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.results) == 0 {
		r.calls++
		return nil, nil
	}

	index := r.calls
	if index >= len(r.results) {
		index = len(r.results) - 1
	}
	result := r.results[index]
	r.calls++

	if result.err != nil {
		return nil, result.err
	}

	if len(result.instances) == 0 {
		return nil, nil
	}

	instances := make([]*solver.StatefulInstance, len(result.instances))
	copy(instances, result.instances)
	return instances, nil
}

func (r *fakeStatefulResolver) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

type fakeTicker struct {
	ch      chan time.Time
	mu      sync.Mutex
	closed  bool
	closedC chan struct{}
}

type fakeBuilder struct {
	scheme string
}

func (b fakeBuilder) Build(grpcresolver.Target, grpcresolver.ClientConn, grpcresolver.BuildOptions) (grpcresolver.Resolver, error) {
	return nil, nil
}

func (b fakeBuilder) Scheme() string {
	return b.scheme
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{ch: make(chan time.Time, 1), closedC: make(chan struct{})}
}

func (t *fakeTicker) Chan() <-chan time.Time {
	return t.ch
}

func (t *fakeTicker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.closed = true
	close(t.closedC)
}

func (t *fakeTicker) Tick() {
	select {
	case t.ch <- time.Now():
	default:
	}
}

func (t *fakeTicker) stopped() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

type fakeClientConn struct {
	mu        sync.Mutex
	updates   []grpcresolver.State
	reported  []error
	updateCh  chan struct{}
	errorCh   chan error
	updateErr error
	// emptyStateErr is returned by UpdateState ONLY for an empty address
	// list — models the real grpc-go LB behavior (pick_first rejects zero
	// addresses with balancer.ErrBadResolverState while non-empty states
	// succeed).
	emptyStateErr error
}

func newFakeClientConn() *fakeClientConn {
	return &fakeClientConn{
		updateCh: make(chan struct{}, 10),
		errorCh:  make(chan error, 10),
	}
}

func (c *fakeClientConn) UpdateState(state grpcresolver.State) error {
	if c.updateErr != nil {
		return c.updateErr
	}
	if c.emptyStateErr != nil && len(state.Addresses) == 0 {
		return c.emptyStateErr
	}

	c.mu.Lock()
	c.updates = append(c.updates, grpcresolver.State{Addresses: state.Addresses})
	c.mu.Unlock()

	select {
	case c.updateCh <- struct{}{}:
	default:
	}

	return nil
}

func (c *fakeClientConn) ReportError(err error) {
	c.mu.Lock()
	c.reported = append(c.reported, err)
	c.mu.Unlock()

	select {
	case c.errorCh <- err:
	default:
	}
}

func (c *fakeClientConn) NewAddress([]grpcresolver.Address) {}

func (c *fakeClientConn) ParseServiceConfig(string) *serviceconfig.ParseResult { return nil }

func (c *fakeClientConn) states() []grpcresolver.State {
	c.mu.Lock()
	defer c.mu.Unlock()

	states := make([]grpcresolver.State, len(c.updates))
	copy(states, c.updates)
	return states
}

func (c *fakeClientConn) waitForUpdate(timeout time.Duration) bool {
	select {
	case <-c.updateCh:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (c *fakeClientConn) waitForError(timeout time.Duration) error {
	select {
	case err := <-c.errorCh:
		return err
	case <-time.After(timeout):
		return nil
	}
}

func (c *fakeClientConn) drainUpdateSignals() {
	for {
		select {
		case <-c.updateCh:
		default:
			return
		}
	}
}

func addressStrings(addresses []grpcresolver.Address) []string {
	if len(addresses) == 0 {
		return nil
	}

	values := make([]string, 0, len(addresses))
	for _, address := range addresses {
		values = append(values, address.Addr)
	}
	return values
}

func newResolverTarget(endpoint string) grpcresolver.Target {
	return grpcresolver.Target{URL: *mustParseResolverURL(Scheme + ":///" + endpoint)}
}

func newStatefulResolverTarget(endpoint string, instance int) grpcresolver.Target {
	u := mustParseResolverURL(fmt.Sprintf("%s:///%s?%s=%d", StatefulScheme, endpoint, instanceQueryParam, instance))
	return grpcresolver.Target{URL: *u}
}

func mustParseResolverURL(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return parsed
}

func newTestLogger() (*bytes.Buffer, *slog.Logger) {
	buf := new(bytes.Buffer)
	handler := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return buf, slog.New(handler)
}

func TestBuildLogsInfo(t *testing.T) {
	buf, logger := newTestLogger()
	logs.SetDefault(logger)

	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeResolverClient{results: []resolveResult{{addresses: []string{"10.0.0.1:50051"}}}}
	builder := NewBuilder(
		WithResolver(client),
		WithNewTicker(func(time.Duration) refreshTicker { return ticker }),
		WithRefreshInterval(time.Hour),
	)

	got, err := builder.Build(newResolverTarget("catalog/grpc:50051"), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	t.Cleanup(got.Close)

	output := buf.String()
	if !strings.Contains(output, "resolver built") {
		t.Fatalf("Build() log output missing 'resolver built', got:\n%s", output)
	}
	if !strings.Contains(output, "catalog/grpc") {
		t.Fatalf("Build() log output missing target 'catalog/grpc', got:\n%s", output)
	}
}

func TestAddressChangeLogsInfo(t *testing.T) {
	buf, logger := newTestLogger()
	logs.SetDefault(logger)

	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeResolverClient{results: []resolveResult{
		{addresses: []string{"10.0.0.1:50051"}},
		{addresses: []string{"10.0.0.1:50051", "10.0.0.2:50051"}},
	}}
	builder := NewBuilder(
		WithResolver(client),
		WithNewTicker(func(time.Duration) refreshTicker { return ticker }),
		WithRefreshInterval(time.Hour),
	)

	got, err := builder.Build(newResolverTarget("catalog/grpc:50051"), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	t.Cleanup(got.Close)
	buf.Reset()

	resolverInstance := got.(*Resolver)
	if err := resolverInstance.Resolve(); err != nil {
		t.Fatalf("Resolve() unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "resolver addresses updated") {
		t.Fatalf("Resolve() log output missing 'resolver addresses updated', got:\n%s", output)
	}
	if !strings.Contains(output, "address_count=2") {
		t.Fatalf("Resolve() log output missing address_count=2, got:\n%s", output)
	}
}

func TestRefreshFailureLogsWarn(t *testing.T) {
	buf, logger := newTestLogger()
	logs.SetDefault(logger)

	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeResolverClient{results: []resolveResult{
		{addresses: []string{"10.0.0.1:50051"}},
		{err: errors.New("temporary failure")},
		{addresses: []string{"10.0.0.2:50051"}},
	}}
	builder := NewBuilder(
		WithResolver(client),
		WithNewTicker(func(time.Duration) refreshTicker { return ticker }),
		WithRefreshInterval(time.Hour),
	)

	got, err := builder.Build(newResolverTarget("catalog/grpc:50051"), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	t.Cleanup(got.Close)
	cc.drainUpdateSignals()
	buf.Reset()

	got.ResolveNow(grpcresolver.ResolveNowOptions{})
	if err := cc.waitForError(time.Second); err == nil {
		t.Fatal("expected error from ResolveNow refresh failure")
	}

	output := buf.String()
	if !strings.Contains(output, "resolver refresh failed") {
		t.Fatalf("refresh() log output missing 'resolver refresh failed', got:\n%s", output)
	}
	if !strings.Contains(output, "WARN") {
		t.Fatalf("refresh() log level not WARN, got:\n%s", output)
	}
}

func TestStableStateNoInfoLog(t *testing.T) {
	buf, logger := newTestLogger()
	logs.SetDefault(logger)

	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeResolverClient{results: []resolveResult{
		{addresses: []string{"10.0.0.1:50051"}},
		{addresses: []string{"10.0.0.1:50051"}},
	}}
	builder := NewBuilder(
		WithResolver(client),
		WithNewTicker(func(time.Duration) refreshTicker { return ticker }),
		WithRefreshInterval(time.Hour),
	)

	got, err := builder.Build(newResolverTarget("catalog/grpc:50051"), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	t.Cleanup(got.Close)
	buf.Reset()

	resolverInstance := got.(*Resolver)
	if err := resolverInstance.Resolve(); err != nil {
		t.Fatalf("Resolve() unexpected error: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "INFO") {
		t.Fatalf("stable state Resolve() should not log INFO, got:\n%s", output)
	}
	if !strings.Contains(output, "resolver addresses unchanged") {
		t.Fatalf("stable state Resolve() should log unchanged at DEBUG, got:\n%s", output)
	}
}

func TestCloseLogsDebug(t *testing.T) {
	buf, logger := newTestLogger()
	logs.SetDefault(logger)

	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeResolverClient{results: []resolveResult{{addresses: []string{"10.0.0.1:50051"}}}}
	builder := NewBuilder(
		WithResolver(client),
		WithNewTicker(func(time.Duration) refreshTicker { return ticker }),
		WithRefreshInterval(time.Hour),
	)

	got, err := builder.Build(newResolverTarget("catalog/grpc:50051"), cc, grpcresolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}

	resolverInstance := got.(*Resolver)
	resolverInstance.Close()

	output := buf.String()
	if !strings.Contains(output, "resolver closed") {
		t.Fatalf("Close() log output missing 'resolver closed', got:\n%s", output)
	}
}
