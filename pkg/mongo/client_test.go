package mongo

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	mongodriver "go.mongodb.org/mongo-driver/mongo"
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

type stubEnvLoader struct {
	env *Environment
	err error
}

func (s *stubEnvLoader) Load(target *Target) (*Environment, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.env, nil
}

type stubK8sClient struct {
	address string
	err     error
}

func (s *stubK8sClient) Resolve(ctx context.Context, target *Target, env *Environment) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.address, nil
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name      string
		rawTarget string
		loader    EnvLoader
		k8sClient K8sClient
		clientErr error
		wantURI   string
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "success",
			rawTarget: "app/mongo-main",
			loader:    &stubEnvLoader{env: &Environment{Name: "dev", App: "app", Namespace: "team-a"}},
			k8sClient: &stubK8sClient{address: net.JoinHostPort("10.0.0.12", "27017")},
			wantURI:   "mongodb://admin:ZOp8SzWTYjjDRAtgSa3MgPeRQ8Zp3aZQ@10.0.0.12:27017/admin?authSource=admin",
		},
		{
			name:      "sanitized service name",
			rawTarget: " GRPC_HELLO.WORLD / mongo@main ",
			loader:    &stubEnvLoader{env: &Environment{Name: " Dev ", App: "GRPC_HELLO.WORLD", Namespace: "team-a"}},
			k8sClient: &stubK8sClient{address: net.JoinHostPort("10.0.0.34", "27017")},
			wantURI:   "mongodb://admin:lJnPUcMYMLzwulQenzwZDlPgim1pydYM@10.0.0.34:27017/admin?authSource=admin",
		},
		{name: "invalid target", rawTarget: "app", loader: &stubEnvLoader{env: &Environment{Name: "dev", App: "app", Namespace: "team-a"}}, wantErr: true, errSubstr: "want app/name"},
		{name: "environment error", rawTarget: "app/mongo-main", loader: &stubEnvLoader{err: errors.New("boom")}, wantErr: true, errSubstr: "boom"},
		{name: "nil environment", rawTarget: "app/mongo-main", loader: &stubEnvLoader{}, wantErr: true, errSubstr: "environment is nil"},
		{name: "k8s resolve error", rawTarget: "app/mongo-main", loader: &stubEnvLoader{env: &Environment{Name: "dev", App: "app", Namespace: "team-a"}}, k8sClient: &stubK8sClient{err: errors.New("resolve failed")}, wantErr: true, errSubstr: "resolve failed"},
		{name: "client creation error", rawTarget: "app/mongo-main", loader: &stubEnvLoader{env: &Environment{Name: "dev", App: "app", Namespace: "team-a"}}, k8sClient: &stubK8sClient{address: net.JoinHostPort("10.0.0.12", "27017")}, clientErr: errors.New("connect failed"), wantErr: true, errSubstr: "connect failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalDefaultEnvLoader := defaultEnvLoader
			originalNewK8sClient := newK8sClient
			originalNewMongoClient := newMongoClient
			var gotURI string
			defaultEnvLoader = tt.loader
			newK8sClient = func() (K8sClient, error) {
				if tt.k8sClient == nil {
					return nil, errors.New("missing k8s client")
				}
				return tt.k8sClient, nil
			}
			newMongoClient = func(uri string) (*mongodriver.Client, error) {
				gotURI = uri
				if tt.clientErr != nil {
					return nil, tt.clientErr
				}
				return new(mongodriver.Client), nil
			}
			t.Cleanup(func() {
				defaultEnvLoader = originalDefaultEnvLoader
				newK8sClient = originalNewK8sClient
				newMongoClient = originalNewMongoClient
			})

			// when
			got, err := NewClient(tt.rawTarget)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewClient(%q) expected error", tt.rawTarget)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("NewClient(%q) error = %v, want substring %q", tt.rawTarget, err, tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewClient(%q) unexpected error: %v", tt.rawTarget, err)
			}
			if got == nil {
				t.Fatalf("NewClient(%q) = nil, want client", tt.rawTarget)
			}
			if gotURI != tt.wantURI {
				t.Fatalf("NewClient(%q) uri = %q, want %q", tt.rawTarget, gotURI, tt.wantURI)
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
	target := &Target{App: "catalog", Name: "mongo-main"}
	env := &Environment{Name: "dev", App: "catalog", Namespace: "team-a"}

	// when
	selector := buildServiceSelector(target, env)

	// then
	parsed, err := labels.Parse(selector)
	if err != nil {
		t.Fatalf("labels.Parse(%q) unexpected error: %v", selector, err)
	}
	if !parsed.Matches(labels.Set{
		serviceAppLabelKey:                 "catalog",
		serviceComponentLabelKey:           "mongo-main",
		serviceDominionAppLabelKey:         "catalog",
		serviceDominionEnvironmentLabelKey: "dev",
	}) {
		t.Fatalf("buildServiceSelector() = %q, selector did not match expected labels", selector)
	}
}

func Test_buildMongoURI(t *testing.T) {
	tests := []struct {
		name   string
		target *Target
		env    *Environment
		addr   string
		want   string
	}{
		{
			name:   "matches deploy naming and password rules",
			target: &Target{App: "app", Name: "mongo-main"},
			env:    &Environment{Name: "dev", App: "app", Namespace: "team-a"},
			addr:   net.JoinHostPort("10.0.0.12", "27017"),
			want:   "mongodb://admin:ZOp8SzWTYjjDRAtgSa3MgPeRQ8Zp3aZQ@10.0.0.12:27017/admin?authSource=admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := buildMongoURI(tt.target, tt.env, tt.addr)

			// then
			if got != tt.want {
				t.Fatalf("buildMongoURI(%#v, %#v) = %q, want %q", tt.target, tt.env, got, tt.want)
			}
		})
	}
}

func TestRuntimeK8sClientResolve(t *testing.T) {
	ready := true
	notReady := false
	terminating := true
	tests := []struct {
		name     string
		seed     []runtime.Object
		reactor  func(action k8stesting.Action) (bool, runtime.Object, error)
		want     string
		wantErr  bool
		errParts []string
	}{
		{
			name: "success returns first ready endpoint in returned order",
			seed: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-dev-mongo-main-bfc75601",
						Namespace: "team-a",
						Labels: map[string]string{
							serviceAppLabelKey:                 "app",
							serviceComponentLabelKey:           "mongo-main",
							serviceDominionAppLabelKey:         "app",
							serviceDominionEnvironmentLabelKey: "dev",
						},
					},
				},
				&discoveryv1.EndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "slice-b",
						Namespace: "team-a",
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "svc-dev-mongo-main-bfc75601",
						},
					},
					Endpoints: []discoveryv1.Endpoint{{
						Conditions: discoveryv1.EndpointConditions{Ready: &ready},
						Addresses:  []string{"10.0.0.20"},
					}},
				},
				&discoveryv1.EndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "slice-a",
						Namespace: "team-a",
						Labels: map[string]string{
							discoveryv1.LabelServiceName: "svc-dev-mongo-main-bfc75601",
						},
					},
					Endpoints: []discoveryv1.Endpoint{{
						Conditions: discoveryv1.EndpointConditions{Ready: &notReady},
						Addresses:  []string{"10.0.0.10"},
					}, {
						Conditions: discoveryv1.EndpointConditions{Ready: &ready},
						Addresses:  []string{"10.0.0.11"},
					}},
				},
			},
			want: net.JoinHostPort("10.0.0.11", "27017"),
		},
		{
			name:     "no services matched",
			seed:     []runtime.Object{},
			wantErr:  true,
			errParts: []string{"no Services matched selector", "team-a"},
		},
		{
			name: "multiple services matched",
			seed: []runtime.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-dev-mongo-main-bfc75601", Namespace: "team-a", Labels: map[string]string{serviceAppLabelKey: "app", serviceComponentLabelKey: "mongo-main", serviceDominionAppLabelKey: "app", serviceDominionEnvironmentLabelKey: "dev"}}},
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-dev-mongo-main-bfc75602", Namespace: "team-a", Labels: map[string]string{serviceAppLabelKey: "app", serviceComponentLabelKey: "mongo-main", serviceDominionAppLabelKey: "app", serviceDominionEnvironmentLabelKey: "dev"}}},
			},
			wantErr:  true,
			errParts: []string{"expected exactly one Service", "svc-dev-mongo-main-bfc75601", "svc-dev-mongo-main-bfc75602"},
		},
		{
			name: "permission denied listing services",
			seed: []runtime.Object{},
			reactor: func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "services"}, "", errors.New("denied"))
			},
			wantErr:  true,
			errParts: []string{"permission denied", "list services"},
		},
		{
			name: "service name mismatch",
			seed: []runtime.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "manual-name", Namespace: "team-a", Labels: map[string]string{serviceAppLabelKey: "app", serviceComponentLabelKey: "mongo-main", serviceDominionAppLabelKey: "app", serviceDominionEnvironmentLabelKey: "dev"}}},
			},
			wantErr:  true,
			errParts: []string{"does not match expected derived name", "manual-name", "svc-dev-mongo-main-bfc75601"},
		},
		{
			name: "permission denied listing endpointslices",
			seed: []runtime.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-dev-mongo-main-bfc75601", Namespace: "team-a", Labels: map[string]string{serviceAppLabelKey: "app", serviceComponentLabelKey: "mongo-main", serviceDominionAppLabelKey: "app", serviceDominionEnvironmentLabelKey: "dev"}}},
			},
			reactor: func(action k8stesting.Action) (bool, runtime.Object, error) {
				if action.GetVerb() != "list" || action.GetResource().Resource != "endpointslices" {
					return false, nil, nil
				}
				return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: discoveryv1.GroupName, Resource: "endpointslices"}, "", errors.New("denied"))
			},
			wantErr:  true,
			errParts: []string{"permission denied", "EndpointSlices"},
		},
		{
			name: "no ready endpoints",
			seed: []runtime.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-dev-mongo-main-bfc75601", Namespace: "team-a", Labels: map[string]string{serviceAppLabelKey: "app", serviceComponentLabelKey: "mongo-main", serviceDominionAppLabelKey: "app", serviceDominionEnvironmentLabelKey: "dev"}}},
				&discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: "slice-a", Namespace: "team-a", Labels: map[string]string{discoveryv1.LabelServiceName: "svc-dev-mongo-main-bfc75601"}}, Endpoints: []discoveryv1.Endpoint{{Conditions: discoveryv1.EndpointConditions{Ready: &notReady}, Addresses: []string{"10.0.0.10"}}, {Conditions: discoveryv1.EndpointConditions{Ready: &ready, Terminating: &terminating}, Addresses: []string{"10.0.0.11"}}}},
			},
			wantErr:  true,
			errParts: []string{"no ready endpoints found", "svc-dev-mongo-main-bfc75601"},
		},
	}

	target := &Target{App: "app", Name: "mongo-main"}
	env := &Environment{Name: "dev", App: "app", Namespace: "team-a"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientset := fake.NewSimpleClientset(tt.seed...)
			if tt.reactor != nil {
				clientset.PrependReactor("list", "services", tt.reactor)
				clientset.PrependReactor("list", "endpointslices", tt.reactor)
			}

			client := &RuntimeK8sClient{clientset: clientset}

			// when
			got, err := client.Resolve(context.Background(), target, env)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Resolve() expected error")
				}
				for _, part := range tt.errParts {
					if !strings.Contains(err.Error(), part) {
						t.Fatalf("Resolve() error = %v, want substring %q", err, part)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Resolve() = %q, want %q", got, tt.want)
			}
		})
	}
}

func Test_includeEndpoint(t *testing.T) {
	ready := true
	notReady := false
	terminating := true
	tests := []struct {
		name     string
		endpoint discoveryv1.Endpoint
		want     bool
	}{
		{name: "nil conditions treated as ready", endpoint: discoveryv1.Endpoint{}, want: true},
		{name: "ready", endpoint: discoveryv1.Endpoint{Conditions: discoveryv1.EndpointConditions{Ready: &ready}}, want: true},
		{name: "not ready", endpoint: discoveryv1.Endpoint{Conditions: discoveryv1.EndpointConditions{Ready: &notReady}}, want: false},
		{name: "terminating", endpoint: discoveryv1.Endpoint{Conditions: discoveryv1.EndpointConditions{Ready: &ready, Terminating: &terminating}}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := includeEndpoint(tt.endpoint)

			// then
			if got != tt.want {
				t.Fatalf("includeEndpoint(%#v) = %t, want %t", tt.endpoint, got, tt.want)
			}
		})
	}
}

func Test_deriveServiceName(t *testing.T) {
	tests := []struct {
		name   string
		target *Target
		env    *Environment
		want   string
	}{
		{name: "normal", target: &Target{App: "app", Name: "mongo-main"}, env: &Environment{Name: "dev", App: "app"}, want: "svc-dev-mongo-main-bfc75601"},
		{name: "normalized", target: &Target{App: " GRPC_HELLO.WORLD ", Name: " mongo@main "}, env: &Environment{Name: " Dev ", App: "GRPC_HELLO.WORLD"}, want: "svc-dev-mongo-main-395bea0a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := deriveServiceName(tt.target, tt.env)

			// then
			if got != tt.want {
				t.Fatalf("deriveServiceName(%#v, %#v) = %q, want %q", tt.target, tt.env, got, tt.want)
			}
		})
	}
}
