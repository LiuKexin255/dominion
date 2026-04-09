package solver

import (
	"context"
	"fmt"
	"sync"
	"time"

	grpcresolver "google.golang.org/grpc/resolver"
)

const (
	defaultRefreshInterval = 30 * time.Second
)

// Scheme is the grpc resolver scheme used by dominion targets.
const Scheme = "dominion"

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
	EnvLoader       EnvLoader
	K8sClient       K8sClient
	NewK8sClient    func() (K8sClient, error)
	NewTicker       func(time.Duration) refreshTicker
	RefreshInterval time.Duration
}

// BuilderOption configures a Builder.
type BuilderOption func(*Builder)

// NewBuilder constructs a Builder with sensible dominion defaults.
func NewBuilder(opts ...BuilderOption) *Builder {
	b := &Builder{
		EnvLoader: new(OSEnvLoader),
		NewK8sClient: func() (K8sClient, error) {
			return NewInClusterClient()
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

// WithEnvLoader overrides the runtime environment loader.
func WithEnvLoader(envLoader EnvLoader) BuilderOption {
	return func(b *Builder) {
		b.EnvLoader = envLoader
	}
}

// WithK8sClient overrides the kubernetes resolver client.
func WithK8sClient(k8sClient K8sClient) BuilderOption {
	return func(b *Builder) {
		b.K8sClient = k8sClient
	}
}

// WithNewK8sClient overrides the kubernetes client factory.
func WithNewK8sClient(newK8sClient func() (K8sClient, error)) BuilderOption {
	return func(b *Builder) {
		b.NewK8sClient = newK8sClient
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

// Resolver polls Kubernetes EndpointSlices and publishes grpc resolver state.
type Resolver struct {
	cc              grpcresolver.ClientConn
	target          *Target
	env             *Environment
	k8sClient       K8sClient
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

// Build creates a dominion grpc polling resolver.
func (b *Builder) Build(target grpcresolver.Target, cc grpcresolver.ClientConn, _ grpcresolver.BuildOptions) (grpcresolver.Resolver, error) {
	parsedTarget, err := ParseTarget(target.Endpoint())
	if err != nil {
		return nil, err
	}

	env, err := b.EnvLoader.Load(parsedTarget)
	if err != nil {
		return nil, err
	}

	k8sClient := b.K8sClient
	if k8sClient == nil {
		k8sClient, err = b.NewK8sClient()
		if err != nil {
			return nil, err
		}
	}

	refreshInterval := b.RefreshInterval

	r := &Resolver{
		cc:              cc,
		target:          parsedTarget,
		env:             env,
		k8sClient:       k8sClient,
		ticker:          b.NewTicker(refreshInterval),
		resolveNowCh:    make(chan struct{}, 1),
		done:            make(chan struct{}),
		refreshInterval: refreshInterval,
	}

	if err := r.Resolve(); err != nil {
		r.ticker.Stop()
		return nil, err
	}

	r.wg.Add(1)
	go r.run()

	return r, nil
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
		r.cc.ReportError(err)
	}
}

// Resolve refreshes the current EndpointSlice-derived address state.
func (r *Resolver) Resolve() error {
	addresses, err := r.k8sClient.Resolve(context.Background(), r.target, r.env)
	if err != nil {
		return err
	}

	stateAddresses := buildResolverAddresses(addresses)
	if r.sameState(stateAddresses) {
		return nil
	}

	if err := r.cc.UpdateState(grpcresolver.State{Addresses: stateAddresses}); err != nil {
		return fmt.Errorf("update resolver state for %q/%q: %w", r.target.App, r.target.Service, err)
	}

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
