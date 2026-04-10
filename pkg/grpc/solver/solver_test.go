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

	grpcresolver "google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    *Target
		wantErr bool
	}{
		{name: "wrapper form", raw: "app/service:50051", want: &Target{App: "app", Service: "service", Port: 50051}},
		{name: "internal form", raw: "dominion:///app/service:50051", want: &Target{App: "app", Service: "service", Port: 50051}},
		{name: "trimmed wrapper form", raw: "  app/service:50051  ", want: &Target{App: "app", Service: "service", Port: 50051}},
		{name: "missing port", raw: "app/service", wantErr: true},
		{name: "non numeric port", raw: "app/service:http", wantErr: true},
		{name: "signed port", raw: "app/service:+50051", wantErr: true},
		{name: "zero port", raw: "app/service:0", wantErr: true},
		{name: "out of range port", raw: "app/service:65536", wantErr: true},
		{name: "missing app", raw: "/service:50051", wantErr: true},
		{name: "missing service", raw: "app/:50051", wantErr: true},
		{name: "extra path segment", raw: "app/service/extra:50051", wantErr: true},
		{name: "alternate internal syntax", raw: "dominion://app/service:50051", wantErr: true},
		{name: "other scheme", raw: "dns:///app/service:50051", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			got, err := ParseTarget(tt.raw)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseTarget(%q) expected error", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTarget(%q) unexpected error: %v", tt.raw, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseTarget(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestRegister(t *testing.T) {
	registerOnce = sync.Once{}
	originalRegisterResolver := registerResolver
	originalNewResolverBuilder := newResolverBuilder
	t.Cleanup(func() {
		registerResolver = originalRegisterResolver
		newResolverBuilder = originalNewResolverBuilder
	})

	var gotBuilders []grpcresolver.Builder
	registerResolver = func(builder grpcresolver.Builder) {
		gotBuilders = append(gotBuilders, builder)
	}
	newResolverBuilder = func() grpcresolver.Builder {
		return fakeBuilder{scheme: Scheme}
	}

	// when
	Register()
	Register()

	// then
	if len(gotBuilders) != 1 {
		t.Fatalf("Register() call count = %d, want 1", len(gotBuilders))
	}
	if gotBuilders[0].Scheme() != Scheme {
		t.Fatalf("Register() scheme = %q, want %q", gotBuilders[0].Scheme(), Scheme)
	}
}

func TestLoadEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		target  *Target
		env     map[string]string
		want    *Environment
		wantErr bool
	}{
		{
			name:   "success",
			target: &Target{App: "app", Service: "service", Port: 50051},
			env: map[string]string{
				dominionAppEnvKey:         "app",
				dominionEnvironmentEnvKey: "dev",
				podNamespaceEnvKey:        "team-a",
			},
			want: &Environment{Name: "dev", App: "app", Namespace: "team-a"},
		},
		{
			name:   "trims env values",
			target: &Target{App: "app", Service: "service", Port: 50051},
			env: map[string]string{
				dominionAppEnvKey:         " app ",
				dominionEnvironmentEnvKey: " dev ",
				podNamespaceEnvKey:        " team-a ",
			},
			want: &Environment{Name: "dev", App: "app", Namespace: "team-a"},
		},
		{name: "missing app", target: &Target{App: "app", Service: "service", Port: 50051}, env: map[string]string{dominionEnvironmentEnvKey: "dev", podNamespaceEnvKey: "team-a"}, wantErr: true},
		{name: "missing environment", target: &Target{App: "app", Service: "service", Port: 50051}, env: map[string]string{dominionAppEnvKey: "app", podNamespaceEnvKey: "team-a"}, wantErr: true},
		{name: "missing namespace", target: &Target{App: "app", Service: "service", Port: 50051}, env: map[string]string{dominionAppEnvKey: "app", dominionEnvironmentEnvKey: "dev"}, wantErr: true},
		{name: "blank app", target: &Target{App: "app", Service: "service", Port: 50051}, env: map[string]string{dominionAppEnvKey: "   ", dominionEnvironmentEnvKey: "dev", podNamespaceEnvKey: "team-a"}, wantErr: true},
		{name: "cross app target rejected", target: &Target{App: "other-app", Service: "service", Port: 50051}, env: map[string]string{dominionAppEnvKey: "app", dominionEnvironmentEnvKey: "dev", podNamespaceEnvKey: "team-a"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLookupEnv := lookupEnv
			t.Cleanup(func() { lookupEnv = originalLookupEnv })
			lookupEnv = func(key string) (string, bool) {
				value, ok := tt.env[key]
				return value, ok
			}

			// when
			got, err := loadEnvironment(tt.target)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("loadEnvironment(%#v) expected error", tt.target)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadEnvironment(%#v) unexpected error: %v", tt.target, err)
			}
			if got == nil {
				t.Fatalf("loadEnvironment(%#v) = nil, want %#v", tt.target, tt.want)
			}
			if *got != *tt.want {
				t.Fatalf("loadEnvironment(%#v) = %#v, want %#v", tt.target, *got, *tt.want)
			}
		})
	}
}

func TestNewInClusterClient(t *testing.T) {
	tests := []struct {
		name      string
		configErr error
		clientErr error
		wantErr   bool
	}{
		{name: "success"},
		{name: "config error", configErr: errors.New("config failure"), wantErr: true},
		{name: "client error", clientErr: errors.New("client failure"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalInClusterConfig := inClusterConfig
			originalNewClientsetForConfig := newClientsetForConfig
			t.Cleanup(func() {
				inClusterConfig = originalInClusterConfig
				newClientsetForConfig = originalNewClientsetForConfig
			})

			wantConfig := &rest.Config{Host: "https://cluster.local"}
			wantClientset := fake.NewSimpleClientset()
			var gotConfig *rest.Config
			clientFactoryCalled := false

			inClusterConfig = func() (*rest.Config, error) {
				if tt.configErr != nil {
					return nil, tt.configErr
				}
				return wantConfig, nil
			}
			newClientsetForConfig = func(config *rest.Config) (kubernetes.Interface, error) {
				clientFactoryCalled = true
				gotConfig = config
				if tt.clientErr != nil {
					return nil, tt.clientErr
				}
				return wantClientset, nil
			}

			// when
			got, err := NewInClusterClient()

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewInClusterClient() expected error")
				}
				if tt.configErr != nil && clientFactoryCalled {
					t.Fatalf("NewInClusterClient() called client factory after config error")
				}
				return
			}
			if err != nil {
				t.Fatalf("NewInClusterClient() unexpected error: %v", err)
			}
			if !clientFactoryCalled {
				t.Fatalf("NewInClusterClient() did not call client factory")
			}
			if got == nil {
				t.Fatalf("NewInClusterClient() = nil, want client")
			}
			if got.clientset != wantClientset {
				t.Fatalf("NewInClusterClient() clientset = %#v, want %#v", got.clientset, wantClientset)
			}
			if gotConfig != wantConfig {
				t.Fatalf("NewInClusterClient() used config %#v, want %#v", gotConfig, wantConfig)
			}
		})
	}
}

func TestBuildServiceSelector(t *testing.T) {
	target := &Target{App: "catalog", Service: "grpc", Port: 50051}
	env := &Environment{Name: "dev", App: "catalog", Namespace: "team-a"}

	// when
	selector := BuildServiceSelector(target, env)

	// then
	parsed, err := labels.Parse(selector)
	if err != nil {
		t.Fatalf("labels.Parse(%q) unexpected error: %v", selector, err)
	}
	if !parsed.Matches(labels.Set{
		serviceAppLabelKey:                 "catalog",
		serviceComponentLabelKey:           "grpc",
		serviceDominionAppLabelKey:         "catalog",
		serviceDominionEnvironmentLabelKey: "dev",
	}) {
		t.Fatalf("BuildServiceSelector() = %q, selector did not match expected labels", selector)
	}
}

func TestServiceLookup(t *testing.T) {
	tests := []struct {
		name     string
		seed     []*corev1.Service
		reactor  func(action k8stesting.Action) (bool, runtime.Object, error)
		want     string
		wantErr  bool
		errParts []string
	}{
		{
			name: "zero match failure",
			seed: []*corev1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-service",
					Namespace: "team-a",
					Labels: map[string]string{
						serviceAppLabelKey:                 "catalog",
						serviceComponentLabelKey:           "http",
						serviceDominionAppLabelKey:         "catalog",
						serviceDominionEnvironmentLabelKey: "dev",
					},
				},
			}},
			wantErr:  true,
			errParts: []string{"no Services matched selector", "team-a"},
		},
		{
			name: "single match success",
			seed: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "catalog-grpc",
						Namespace: "team-a",
						Labels: map[string]string{
							serviceAppLabelKey:                 "catalog",
							serviceComponentLabelKey:           "grpc",
							serviceDominionAppLabelKey:         "catalog",
							serviceDominionEnvironmentLabelKey: "dev",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "catalog-grpc-other-namespace",
						Namespace: "team-b",
						Labels: map[string]string{
							serviceAppLabelKey:                 "catalog",
							serviceComponentLabelKey:           "grpc",
							serviceDominionAppLabelKey:         "catalog",
							serviceDominionEnvironmentLabelKey: "dev",
						},
					},
				},
			},
			want: "catalog-grpc",
		},
		{
			name: "multiple match failure",
			seed: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "catalog-grpc-a",
						Namespace: "team-a",
						Labels: map[string]string{
							serviceAppLabelKey:                 "catalog",
							serviceComponentLabelKey:           "grpc",
							serviceDominionAppLabelKey:         "catalog",
							serviceDominionEnvironmentLabelKey: "dev",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "catalog-grpc-b",
						Namespace: "team-a",
						Labels: map[string]string{
							serviceAppLabelKey:                 "catalog",
							serviceComponentLabelKey:           "grpc",
							serviceDominionAppLabelKey:         "catalog",
							serviceDominionEnvironmentLabelKey: "dev",
						},
					},
				},
			},
			wantErr:  true,
			errParts: []string{"expected exactly one Service", "catalog-grpc-a", "catalog-grpc-b"},
		},
		{
			name: "permission list failure handling",
			reactor: func(action k8stesting.Action) (bool, runtime.Object, error) {
				listAction, ok := action.(k8stesting.ListAction)
				if !ok {
					return false, nil, nil
				}
				if action.GetResource().Resource != "services" {
					return false, nil, nil
				}
				return true, nil, apierrors.NewForbidden(
					schema.GroupResource{Resource: "services"},
					listAction.GetNamespace(),
					fmt.Errorf("rbac denied"),
				)
			},
			wantErr:  true,
			errParts: []string{"permission denied", "rbac denied"},
		},
	}

	target := &Target{App: "catalog", Service: "grpc", Port: 50051}
	env := &Environment{Name: "dev", App: "catalog", Namespace: "team-a"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientset := fake.NewSimpleClientset()
			for _, service := range tt.seed {
				if err := clientset.Tracker().Add(service.DeepCopy()); err != nil {
					t.Fatalf("seed service %s/%s failed: %v", service.Namespace, service.Name, err)
				}
			}
			if tt.reactor != nil {
				clientset.PrependReactor("list", "services", tt.reactor)
			}

			client := &RuntimeK8sClient{clientset: clientset}

			// when
			got, err := client.Lookup(t.Context(), target, env)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Lookup() expected error")
				}
				for _, part := range tt.errParts {
					if !strings.Contains(err.Error(), part) {
						t.Fatalf("Lookup() error = %q, want substring %q", err.Error(), part)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("Lookup() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Lookup() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEndpointSliceResolve(t *testing.T) {
	trueValue := true
	falseValue := false

	tests := []struct {
		name   string
		slices []*discoveryv1.EndpointSlice
		want   []string
	}{
		{
			name: "multi slice union dedupe stable sort",
			slices: []*discoveryv1.EndpointSlice{
				newEndpointSlice("team-a", "slice-a", "catalog-grpc", []discoveryv1.Endpoint{
					{Addresses: []string{"10.0.0.2"}},
					{Addresses: []string{"10.0.0.1"}},
				}),
				newEndpointSlice("team-a", "slice-b", "catalog-grpc", []discoveryv1.Endpoint{
					{Addresses: []string{"10.0.0.3", "10.0.0.2"}},
				}),
			},
			want: []string{"10.0.0.1:50051", "10.0.0.2:50051", "10.0.0.3:50051"},
		},
		{
			name: "readiness filtering and terminating exclusion",
			slices: []*discoveryv1.EndpointSlice{
				newEndpointSlice("team-a", "slice-a", "catalog-grpc", []discoveryv1.Endpoint{
					{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &trueValue}},
					{Addresses: []string{"10.0.0.2"}, Conditions: discoveryv1.EndpointConditions{Ready: &falseValue}},
					{Addresses: []string{"10.0.0.3"}, Conditions: discoveryv1.EndpointConditions{Terminating: &trueValue}},
					{Addresses: []string{"10.0.0.4"}, Conditions: discoveryv1.EndpointConditions{}},
				}),
			},
			want: []string{"10.0.0.1:50051", "10.0.0.4:50051"},
		},
		{
			name: "empty endpoint publication",
			slices: []*discoveryv1.EndpointSlice{
				newEndpointSlice("team-a", "slice-a", "catalog-grpc", []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.2"}, Conditions: discoveryv1.EndpointConditions{Ready: &falseValue}}}),
			},
		},
	}

	target := &Target{App: "catalog", Service: "grpc", Port: 50051}
	env := &Environment{Name: "dev", App: "catalog", Namespace: "team-a"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientset := fake.NewSimpleClientset(newService("team-a", "catalog-grpc", map[string]string{
				serviceAppLabelKey:                 "catalog",
				serviceComponentLabelKey:           "grpc",
				serviceDominionAppLabelKey:         "catalog",
				serviceDominionEnvironmentLabelKey: "dev",
			}))
			for _, endpointSlice := range tt.slices {
				if err := clientset.Tracker().Add(endpointSlice.DeepCopy()); err != nil {
					t.Fatalf("seed EndpointSlice %s/%s failed: %v", endpointSlice.Namespace, endpointSlice.Name, err)
				}
			}

			client := &RuntimeK8sClient{clientset: clientset}

			// when
			got, err := client.Resolve(t.Context(), target, env)

			// then
			if err != nil {
				t.Fatalf("Resolve() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Resolve() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestResolverInitialResolveSuccess(t *testing.T) {
	cc := newFakeClientConn()
	ticker := newFakeTicker()
	client := &fakeResolverK8sClient{results: []resolveResult{{addresses: []string{"10.0.0.1:50051", "10.0.0.2:50051"}}}}
	builder := NewBuilder(
		WithEnvLoader(staticEnvLoader{env: &Environment{Name: "dev", App: "catalog", Namespace: "team-a"}}),
		WithK8sClient(client),
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
	client := &fakeResolverK8sClient{results: []resolveResult{{addresses: []string{"10.0.0.1:50051"}}, {addresses: []string{"10.0.0.1:50051"}}}}
	builder := NewBuilder(
		WithEnvLoader(staticEnvLoader{env: &Environment{Name: "dev", App: "catalog", Namespace: "team-a"}}),
		WithK8sClient(client),
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
	client := &fakeResolverK8sClient{results: []resolveResult{{addresses: []string{"10.0.0.1:50051"}}, {addresses: []string{"10.0.0.1:50051", "10.0.0.2:50051"}}}}
	builder := NewBuilder(
		WithEnvLoader(staticEnvLoader{env: &Environment{Name: "dev", App: "catalog", Namespace: "team-a"}}),
		WithK8sClient(client),
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
	client := &fakeResolverK8sClient{results: []resolveResult{{addresses: []string{"10.0.0.1:50051"}}, {err: errors.New("temporary list failure")}, {addresses: []string{"10.0.0.2:50051"}}}}
	builder := NewBuilder(
		WithEnvLoader(staticEnvLoader{env: &Environment{Name: "dev", App: "catalog", Namespace: "team-a"}}),
		WithK8sClient(client),
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
	client := &fakeResolverK8sClient{results: []resolveResult{{addresses: []string{"10.0.0.1:50051"}}, {addresses: []string{"10.0.0.2:50051"}}}}
	builder := NewBuilder(
		WithEnvLoader(staticEnvLoader{env: &Environment{Name: "dev", App: "catalog", Namespace: "team-a"}}),
		WithK8sClient(client),
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
	client := &fakeResolverK8sClient{results: []resolveResult{{addresses: []string{"10.0.0.1:50051"}}, {addresses: []string{"10.0.0.2:50051"}}}}
	builder := NewBuilder(
		WithEnvLoader(staticEnvLoader{env: &Environment{Name: "dev", App: "catalog", Namespace: "team-a"}}),
		WithK8sClient(client),
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

type staticEnvLoader struct {
	env *Environment
}

func (l staticEnvLoader) Load(*Target) (*Environment, error) {
	return l.env, nil
}

type resolveResult struct {
	addresses []string
	err       error
}

type fakeResolverK8sClient struct {
	mu      sync.Mutex
	results []resolveResult
	calls   int
}

func (c *fakeResolverK8sClient) Lookup(context.Context, *Target, *Environment) (string, error) {
	return "", nil
}

func (c *fakeResolverK8sClient) Resolve(context.Context, *Target, *Environment) ([]string, error) {
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

func (c *fakeResolverK8sClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
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

func mustParseResolverURL(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return parsed
}

func newService(namespace, name string, labels map[string]string) *corev1.Service {
	return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}}
}

func newEndpointSlice(namespace, name, serviceName string, endpoints []discoveryv1.Endpoint) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: serviceName,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints:   endpoints,
	}
}
