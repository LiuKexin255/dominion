package solver

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/solver"

	"google.golang.org/grpc/balancer"
	grpcresolver "google.golang.org/grpc/resolver"
)

const (
	defaultRefreshInterval = 30 * time.Second
	instanceQueryParam     = "instance"
)

// Scheme is the grpc resolver scheme used by dominion targets.
const Scheme = "dominion"

// refreshTicker abstracts time.Ticker for resolver refresh loops.
type refreshTicker interface {
	Chan() <-chan time.Time
	Stop()
}

type runtimeTicker struct {
	*timeticker
}

type timeticker = time.Ticker

func (t *runtimeTicker) Chan() <-chan time.Time {
	return t.C
}

// Builder builds dominion grpc resolvers.
type Builder struct {
	Resolver        solver.Resolver
	NewResolver     func() (solver.Resolver, error)
	NewTicker       func(time.Duration) refreshTicker
	RefreshInterval time.Duration
}

// StatefulBuilder builds gRPC resolvers for stateful service instances.
// It uses the "dominion-stateful" scheme and resolves individual instances
// of a stateful service via StatefulResolver.
type StatefulBuilder struct {
	StatefulResolver    solver.StatefulResolver
	NewStatefulResolver func() (solver.StatefulResolver, error)
	NewTicker           func(time.Duration) refreshTicker
	RefreshInterval     time.Duration
}

// BuilderOption configures a Builder.
type BuilderOption func(*Builder)

// StatefulBuilderOption configures a StatefulBuilder.
type StatefulBuilderOption func(*StatefulBuilder)

// NewBuilder constructs a Builder with sensible dominion defaults.
func NewBuilder(opts ...BuilderOption) *Builder {
	b := &Builder{
		NewResolver: func() (solver.Resolver, error) {
			return solver.NewDeployResolver()
		},
		NewTicker: func(d time.Duration) refreshTicker {
			return &runtimeTicker{timeticker: time.NewTicker(d)}
		},
		RefreshInterval: defaultRefreshInterval,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(b)
		}
	}

	return b
}

// NewStatefulBuilder constructs a StatefulBuilder with sensible defaults.
func NewStatefulBuilder(opts ...StatefulBuilderOption) *StatefulBuilder {
	b := &StatefulBuilder{
		NewStatefulResolver: func() (solver.StatefulResolver, error) {
			return solver.NewDeployStatefulResolver()
		},
		NewTicker: func(d time.Duration) refreshTicker {
			return &runtimeTicker{timeticker: time.NewTicker(d)}
		},
		RefreshInterval: defaultRefreshInterval,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(b)
		}
	}

	return b
}

// WithResolver overrides the resolver implementation.
func WithResolver(resolver solver.Resolver) BuilderOption {
	return func(b *Builder) {
		b.Resolver = resolver
	}
}

// WithNewResolver overrides the resolver factory.
func WithNewResolver(newResolver func() (solver.Resolver, error)) BuilderOption {
	return func(b *Builder) {
		b.NewResolver = newResolver
	}
}

// WithStatefulResolver overrides the stateful resolver implementation.
func WithStatefulResolver(resolver solver.StatefulResolver) StatefulBuilderOption {
	return func(b *StatefulBuilder) {
		b.StatefulResolver = resolver
	}
}

// WithNewStatefulResolver overrides the stateful resolver factory.
func WithNewStatefulResolver(newResolver func() (solver.StatefulResolver, error)) StatefulBuilderOption {
	return func(b *StatefulBuilder) {
		b.NewStatefulResolver = newResolver
	}
}

// WithNewTicker overrides the refresh ticker factory.
func WithNewTicker(newTicker func(time.Duration) refreshTicker) BuilderOption {
	return func(b *Builder) {
		b.NewTicker = newTicker
	}
}

// WithRefreshInterval overrides the resolver refresh interval.
func WithRefreshInterval(refreshInterval time.Duration) BuilderOption {
	return func(b *Builder) {
		b.RefreshInterval = refreshInterval
	}
}

// Resolver polls service endpoints and publishes grpc resolver state.
type Resolver struct {
	cc              grpcresolver.ClientConn
	target          *solver.Target
	resolver        solver.Resolver
	ticker          refreshTicker
	resolveNowCh    chan struct{}
	done            chan struct{}
	refreshInterval time.Duration

	mu        sync.Mutex
	lastAddrs []grpcresolver.Address
	hasState  bool
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// Scheme returns the dominion grpc resolver scheme.
func (b *Builder) Scheme() string {
	return Scheme
}

// Scheme returns the dominion-stateful grpc resolver scheme.
func (b *StatefulBuilder) Scheme() string {
	return StatefulScheme
}

// Build creates a dominion grpc polling resolver.
func (b *Builder) Build(target grpcresolver.Target, cc grpcresolver.ClientConn, _ grpcresolver.BuildOptions) (grpcresolver.Resolver, error) {
	parsedTarget, err := solver.ParseTarget(target.Endpoint())
	if err != nil {
		return nil, err
	}

	resolver := b.Resolver
	if resolver == nil {
		resolver, err = b.NewResolver()
		if err != nil {
			return nil, err
		}
	}

	refreshInterval := b.RefreshInterval

	r := &Resolver{
		cc:              cc,
		target:          parsedTarget,
		resolver:        resolver,
		ticker:          b.NewTicker(refreshInterval),
		resolveNowCh:    make(chan struct{}, 1),
		done:            make(chan struct{}),
		refreshInterval: refreshInterval,
	}

	if err := r.Resolve(); err != nil {
		r.ticker.Stop()
		return nil, err
	}

	logs.Info(context.Background(), "resolver built",
		event.String("scheme", Scheme),
		event.String("target", fmt.Sprintf("%s/%s", parsedTarget.App, parsedTarget.Service)),
		event.Any("refresh_interval", refreshInterval),
	)

	r.wg.Add(1)
	go r.run()

	return r, nil
}

// Build creates a dominion-stateful grpc polling resolver.
func (b *StatefulBuilder) Build(target grpcresolver.Target, cc grpcresolver.ClientConn, _ grpcresolver.BuildOptions) (grpcresolver.Resolver, error) {
	instanceStr := target.URL.Query().Get(instanceQueryParam)
	if instanceStr == "" {
		return nil, fmt.Errorf("missing required query parameter %q", instanceQueryParam)
	}

	instanceIndex, err := strconv.Atoi(instanceStr)
	if err != nil {
		return nil, fmt.Errorf("invalid instance parameter %q: %w", instanceStr, err)
	}

	parsedTarget, err := solver.ParseTarget(strings.TrimPrefix(target.URL.Host+target.URL.Path, "/"))
	if err != nil {
		return nil, err
	}

	resolver := b.StatefulResolver
	if resolver == nil {
		resolver, err = b.NewStatefulResolver()
		if err != nil {
			return nil, err
		}
	}

	refreshInterval := b.RefreshInterval

	// Build creates a fresh adapter per target so each resolver instance keeps
	// its own ordinal index while safely sharing the underlying stateless resolver.
	r := &Resolver{
		cc:              cc,
		target:          parsedTarget,
		resolver:        &statefulResolverAdapter{stateful: resolver, index: instanceIndex},
		ticker:          b.NewTicker(refreshInterval),
		resolveNowCh:    make(chan struct{}, 1),
		done:            make(chan struct{}),
		refreshInterval: refreshInterval,
	}

	if err := r.Resolve(); err != nil {
		r.ticker.Stop()
		return nil, err
	}

	logs.Info(context.Background(), "stateful resolver built",
		event.String("scheme", StatefulScheme),
		event.String("target", fmt.Sprintf("%s/%s", parsedTarget.App, parsedTarget.Service)),
		event.Int("instance", instanceIndex),
		event.Any("refresh_interval", refreshInterval),
	)

	r.wg.Add(1)
	go r.run()

	return r, nil
}

// statefulResolverAdapter adapts a StatefulResolver to the Resolver interface
// by resolving all instances and filtering to the requested instance index.
type statefulResolverAdapter struct {
	stateful solver.StatefulResolver
	index    int
}

// Resolve resolves the target to the requested stateful instance endpoints.
//
// A missing instance / an instance without ready endpoints is a LEGAL state —
// the service is not yet provisioned or its endpoints are momentarily
// unavailable (e.g. a stateful rollout window while the EndpointSlice is
// empty, deploy incident 2026-08-09). It is published as an EMPTY address
// list (`UpdateState` with no addresses → the LB policy reports
// TRANSIENT_FAILURE so RPCs fail fast), NOT reported as a resolver error.
// This matches the grpc-go reference resolvers: the DNS resolver publishes
// empty lookup results via `UpdateState` and only calls `ReportError` when
// the lookup itself fails (grpc-go `internal/resolver/dns/dns_resolver.go`),
// and xDS/EDS publish empty endpoint states the same way. The polling loop
// keeps re-resolving, so the channel self-heals on the next refresh.
func (a *statefulResolverAdapter) Resolve(ctx context.Context, target *solver.Target) ([]string, error) {
	instances, err := a.stateful.Resolve(ctx, target)
	if err != nil {
		return nil, err
	}

	for _, instance := range instances {
		if instance.Index != a.index {
			continue
		}
		// The instance exists but has no ready endpoints yet (terminating /
		// not-ready pod): publish empty — a transient state, not an error.
		if len(instance.Endpoints) == 0 {
			return nil, nil
		}
		return instance.Endpoints, nil
	}

	// The requested instance does not exist (yet) — possibly mid-rollout:
	// publish empty; the polling loop re-resolves on the next refresh.
	return nil, nil
}

func (r *Resolver) run() {
	defer r.wg.Done()

	for {
		select {
		case <-r.done:
			return
		case <-r.ticker.Chan():
			r.refresh()
		case <-r.resolveNowCh:
			r.refresh()
		}
	}
}

func (r *Resolver) refresh() {
	if err := r.Resolve(); err != nil {
		r.mu.Lock()
		hasState := r.hasState
		r.mu.Unlock()

		if hasState {
			logs.Warn(context.Background(), "resolver refresh failed",
				event.String("target", fmt.Sprintf("%s/%s", r.target.App, r.target.Service)),
				event.Err(err),
			)
		}
		r.cc.ReportError(err)
	}
}

// Resolve refreshes the current EndpointSlice-derived address state.
func (r *Resolver) Resolve() error {
	targetStr := fmt.Sprintf("%s/%s", r.target.App, r.target.Service)

	addresses, err := r.resolver.Resolve(context.Background(), r.target)
	if err != nil {
		r.mu.Lock()
		hasState := r.hasState
		r.mu.Unlock()

		if !hasState {
			logs.Error(context.Background(), "resolver initial resolve failed",
				event.String("target", targetStr),
				event.Err(err),
			)
		}
		return err
	}

	stateAddresses := buildResolverAddresses(addresses)
	if r.sameState(stateAddresses) {
		logs.Debug(context.Background(), "resolver addresses unchanged",
			event.String("target", targetStr),
		)
		return nil
	}

	if err := r.cc.UpdateState(grpcresolver.State{Addresses: stateAddresses}); err != nil {
		// An empty address list is a LEGAL resolution result (the service is
		// not yet provisioned, or its endpoints are momentarily unavailable —
		// e.g. a stateful rollout window, deploy incident 2026-08-09). The
		// grpc-go LB layer (pick_first etc.) rejects it with
		// `balancer.ErrBadResolverState`: the channel then reports
		// TRANSIENT_FAILURE ("produced zero addresses") so RPCs fail fast,
		// which is the desired behavior — NOT a resolver failure. Report
		// other UpdateState errors, but treat the empty-result rejection as
		// expected: the polling loop keeps re-resolving and self-heals once
		// the instance becomes resolvable again (without this, the Build
		// path's initial resolve would fail on the empty window and
		// permanently kill the channel — the resolver never gets a chance to
		// recover).
		if !errors.Is(err, balancer.ErrBadResolverState) {
			logs.Error(context.Background(), "resolver update state failed",
				event.String("target", targetStr),
				event.Err(err),
			)
			return fmt.Errorf("update resolver state for %q/%q: %w", r.target.App, r.target.Service, err)
		}
		logs.Debug(context.Background(), "resolver published empty state (LB rejected zero addresses, expected)",
			event.String("target", targetStr),
		)
		// Record the (rejected) empty state anyway, so subsequent polls with
		// no change skip the pointless UpdateState round-trip (grpc-go
		// issue #5048: "If it polls and finds no change, it would be fine to
		// not call UpdateState with the data").
		r.storeState(stateAddresses)
		return nil
	}

	logs.Info(context.Background(), "resolver addresses updated",
		event.String("target", targetStr),
		event.Int("address_count", len(stateAddresses)),
	)

	r.storeState(stateAddresses)
	return nil
}

// ResolveNow triggers a best-effort immediate refresh.
func (r *Resolver) ResolveNow(grpcresolver.ResolveNowOptions) {
	select {
	case r.resolveNowCh <- struct{}{}:
	default:
	}
}

// Close stops the polling loop and releases resolver resources.
func (r *Resolver) Close() {
	r.closeOnce.Do(func() {
		logs.Debug(context.Background(), "resolver closed",
			event.String("target", fmt.Sprintf("%s/%s", r.target.App, r.target.Service)),
		)
		close(r.done)
		r.ticker.Stop()
		r.wg.Wait()
	})
}

func buildResolverAddresses(addresses []string) []grpcresolver.Address {
	if len(addresses) == 0 {
		return nil
	}

	resolved := make([]grpcresolver.Address, 0, len(addresses))
	for _, address := range addresses {
		resolved = append(resolved, grpcresolver.Address{Addr: address})
	}

	return resolved
}

func (r *Resolver) sameState(addresses []grpcresolver.Address) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.hasState {
		return false
	}

	if len(r.lastAddrs) != len(addresses) {
		return false
	}

	for i := range addresses {
		if r.lastAddrs[i].Addr != addresses[i].Addr {
			return false
		}
	}

	return true
}

func (r *Resolver) storeState(addresses []grpcresolver.Address) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastAddrs = addresses
	r.hasState = true
}
