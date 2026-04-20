package solver

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"dominion/pkg/solver"

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
		results: []statefulResolveResult{{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 0, Endpoints: []string{"10.0.0.1:50051"}}, &solver.StatefulInstance{Index: 1, Endpoints: []string{"10.0.0.2:50051"}}}}},
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

func TestStatefulBuilder_InstanceNotFound(t *testing.T) {
	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeStatefulResolver{
		results: []statefulResolveResult{
			{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 5, Endpoints: []string{"10.0.0.5:50051"}}}},
			{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 0, Endpoints: []string{"10.0.0.1:50051"}}, &solver.StatefulInstance{Index: 1, Endpoints: []string{"10.0.0.2:50051"}}}},
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

	got.ResolveNow(grpcresolver.ResolveNowOptions{})
	reportedErr := cc.waitForError(time.Second)
	if reportedErr == nil {
		t.Fatal("ReportError() = nil, want stateful instance not found")
	}
	if !errors.Is(reportedErr, solver.ErrInstanceNotFound) {
		t.Fatalf("ReportError() = %v, want error wrapping %v", reportedErr, solver.ErrInstanceNotFound)
	}
	if len(cc.states()) != 1 {
		t.Fatalf("after error update count = %d, want 1", len(cc.states()))
	}
}

func TestStatefulBuilder_InstanceNoReadyEndpoints(t *testing.T) {
	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeStatefulResolver{
		results: []statefulResolveResult{
			{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 1, Endpoints: []string{"10.0.0.1:50051"}}}},
			{instances: []*solver.StatefulInstance{&solver.StatefulInstance{Index: 1}}},
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
	reportedErr := cc.waitForError(time.Second)
	if reportedErr == nil {
		t.Fatal("ReportError() = nil, want stateful instance has no ready endpoints")
	}
	if !errors.Is(reportedErr, solver.ErrInstanceNoReadyEndpoints) {
		t.Fatalf("ReportError() = %v, want error wrapping %v", reportedErr, solver.ErrInstanceNoReadyEndpoints)
	}
	if len(cc.states()) != 1 {
		t.Fatalf("after error update count = %d, want 1", len(cc.states()))
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
