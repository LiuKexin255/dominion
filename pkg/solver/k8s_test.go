package solver

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

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

var (
	sharedTrue  = true
	sharedFalse = false
)

func Test_buildServiceSelector(t *testing.T) {
	tests := []struct {
		name  string
		given struct {
			target *Target
			env    *environment
		}
		want string
	}{
		{
			name: "standard target and environment",
			given: struct {
				target *Target
				env    *environment
			}{
				target: &Target{App: "app-a", Service: "service-a"},
				env:    &environment{App: "app-a", Name: "dev"},
			},
			want: "app.kubernetes.io/component=service-a,app.kubernetes.io/name=app-a,dominion.io/app=app-a,dominion.io/environment=dev",
		},
		{
			name: "different target and app values",
			given: struct {
				target *Target
				env    *environment
			}{
				target: &Target{App: "app-b", Service: "service-b"},
				env:    &environment{App: "app-b", Name: "prod"},
			},
			want: "app.kubernetes.io/component=service-b,app.kubernetes.io/name=app-b,dominion.io/app=app-b,dominion.io/environment=prod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := buildServiceSelector(tt.given.target, tt.given.env)

			// then
			if got != tt.want {
				t.Fatalf("buildServiceSelector(%#v, %#v) = %q, want %q", tt.given.target, tt.given.env, got, tt.want)
			}
			if _, err := labels.Parse(got); err != nil {
				t.Fatalf("buildServiceSelector(%#v, %#v) produced unparseable selector %q: %v", tt.given.target, tt.given.env, got, err)
			}
		})
	}
}

func Test_buildServiceSelectorDifferentInputsProduceDifferentSelectors(t *testing.T) {
	// given
	first := buildServiceSelector(&Target{App: "app-a", Service: "service-a"}, &environment{App: "app-a", Name: "dev"})
	second := buildServiceSelector(&Target{App: "app-b", Service: "service-b"}, &environment{App: "app-b", Name: "prod"})

	// when / then
	if first == second {
		t.Fatalf("selectors should differ: %q == %q", first, second)
	}
}

func TestNewK8sResolver(t *testing.T) {
	originalInClusterConfig := inClusterConfig
	originalNewClientsetForConfig := newClientsetForConfig
	t.Cleanup(func() {
		inClusterConfig = originalInClusterConfig
		newClientsetForConfig = originalNewClientsetForConfig
	})

	tests := []struct {
		name  string
		given struct {
			configErr    error
			clientsetErr error
		}
		wantErr string
	}{
		{
			name: "config load fails",
			given: struct {
				configErr    error
				clientsetErr error
			}{
				configErr: errors.New("config unavailable"),
			},
			wantErr: "build in-cluster kubernetes config",
		},
		{
			name: "clientset creation fails",
			given: struct {
				configErr    error
				clientsetErr error
			}{
				clientsetErr: errors.New("clientset unavailable"),
			},
			wantErr: "build kubernetes clientset",
		},
		{name: "success"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			configCalls := 0
			clientsetCalls := 0
			inClusterConfig = func() (*rest.Config, error) {
				configCalls++
				if tt.given.configErr != nil {
					return nil, tt.given.configErr
				}
				return &rest.Config{Host: "https://kubernetes.default.svc"}, nil
			}
			newClientsetForConfig = func(config *rest.Config) (kubernetes.Interface, error) {
				clientsetCalls++
				if tt.given.clientsetErr != nil {
					return nil, tt.given.clientsetErr
				}
				return fake.NewSimpleClientset(), nil
			}

			// when
			got, err := NewK8sResolver()

			// then
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NewK8sResolver() error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewK8sResolver() unexpected error: %v", err)
			}
			if got == nil {
				t.Fatal("NewK8sResolver() returned nil client")
			}
			if got.clientset == nil {
				t.Fatal("NewK8sResolver() returned nil clientset")
			}
			if configCalls != 1 {
				t.Fatalf("inClusterConfig calls = %d, want 1", configCalls)
			}
			if clientsetCalls != 1 {
				t.Fatalf("newClientsetForConfig calls = %d, want 1", clientsetCalls)
			}
		})
	}
}

func TestK8sResolverLookup(t *testing.T) {
	tests := []struct {
		name  string
		given struct {
			services []runtime.Object
			reactor  func(*fake.Clientset)
		}
		want    string
		wantErr string
	}{
		{
			name: "single matching service",
			given: struct {
				services []runtime.Object
				reactor  func(*fake.Clientset)
			}{
				services: []runtime.Object{
					serviceObject("target-service", map[string]string{
						ServiceAppLabelKey:                 "billing",
						ServiceComponentLabelKey:           "api",
						ServiceDominionAppLabelKey:         "billing",
						ServiceDominionEnvironmentLabelKey: "dev",
					}),
				},
			},
			want: "target-service",
		},
		{
			name:    "no matching services",
			wantErr: "no Services matched selector",
		},
		{
			name: "multiple matching services",
			given: struct {
				services []runtime.Object
				reactor  func(*fake.Clientset)
			}{
				services: []runtime.Object{
					serviceObject("service-b", map[string]string{
						ServiceAppLabelKey:                 "billing",
						ServiceComponentLabelKey:           "api",
						ServiceDominionAppLabelKey:         "billing",
						ServiceDominionEnvironmentLabelKey: "dev",
					}),
					serviceObject("service-a", map[string]string{
						ServiceAppLabelKey:                 "billing",
						ServiceComponentLabelKey:           "api",
						ServiceDominionAppLabelKey:         "billing",
						ServiceDominionEnvironmentLabelKey: "dev",
					}),
				},
			},
			wantErr: "found 2 (service-a, service-b)",
		},
		{
			name: "permission denied",
			given: struct {
				services []runtime.Object
				reactor  func(*fake.Clientset)
			}{
				reactor: func(clientset *fake.Clientset) {
					clientset.PrependReactor("list", "services", func(action k8stesting.Action) (bool, runtime.Object, error) {
						return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "services"}, "", errors.New("denied"))
					})
				},
			},
			wantErr: "permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLookupEnv := lookupEnv
			t.Cleanup(func() {
				lookupEnv = originalLookupEnv
			})
			lookupEnv = func(key string) (string, bool) {
				switch key {
				case serviceAppEnvKey:
					return "billing", true
				case dominionEnvironmentEnvKey:
					return "dev", true
				case podNamespaceEnvKey:
					return "default", true
				default:
					return "", false
				}
			}

			// given
			clientset := fake.NewSimpleClientset()
			for _, object := range tt.given.services {
				if err := clientset.Tracker().Add(object); err != nil {
					t.Fatalf("Tracker().Add() error = %v", err)
				}
			}
			if tt.given.reactor != nil {
				tt.given.reactor(clientset)
			}

			client := &K8sResolver{clientset: clientset}
			target := &Target{App: "billing", Service: "api"}

			// when
			got, err := client.Lookup(context.Background(), target)

			// then
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Lookup() error = %v, want substring %q", err, tt.wantErr)
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

func TestK8sResolverResolveEndpoints(t *testing.T) {
	tests := []struct {
		name  string
		given struct {
			targetPort     int
			endpointSlices []runtime.Object
			reactor        func(*fake.Clientset)
		}
		want    []string
		wantErr string
	}{
		{
			name: "ready endpoints use target port and are deduplicated and sorted",
			given: struct {
				targetPort     int
				endpointSlices []runtime.Object
				reactor        func(*fake.Clientset)
			}{
				targetPort: 8443,
				endpointSlices: []runtime.Object{
					endpointSliceObject("service-a", []int32{50051}, []discoveryv1.Endpoint{
						{Addresses: []string{"10.0.0.2", "10.0.0.1"}},
						{Addresses: []string{"10.0.0.1"}},
						{Addresses: []string{"10.0.0.3"}, Conditions: discoveryv1.EndpointConditions{Ready: boolValuePtr(false)}},
						{Addresses: []string{"10.0.0.4"}, Conditions: discoveryv1.EndpointConditions{Terminating: boolValuePtr(true)}},
					}),
				},
			},
			want: []string{"10.0.0.1:8443", "10.0.0.2:8443"},
		},
		{
			name: "target port zero uses endpoint slice ports",
			given: struct {
				targetPort     int
				endpointSlices []runtime.Object
				reactor        func(*fake.Clientset)
			}{
				endpointSlices: []runtime.Object{
					endpointSliceObject("service-a", []int32{27017}, []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.10"}}}),
					endpointSliceObject("service-a", []int32{27018}, []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.11"}}}),
				},
			},
			want: []string{"10.0.0.10:27017", "10.0.0.11:27018"},
		},
		{
			name: "no ready endpoints returns nil",
			given: struct {
				targetPort     int
				endpointSlices []runtime.Object
				reactor        func(*fake.Clientset)
			}{
				endpointSlices: []runtime.Object{
					endpointSliceObject("service-a", []int32{8080}, []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.5"}, Conditions: discoveryv1.EndpointConditions{Ready: boolValuePtr(false)}}}),
				},
			},
		},
		{
			name: "permission denied",
			given: struct {
				targetPort     int
				endpointSlices []runtime.Object
				reactor        func(*fake.Clientset)
			}{
				reactor: func(clientset *fake.Clientset) {
					clientset.PrependReactor("list", "endpointslices", func(action k8stesting.Action) (bool, runtime.Object, error) {
						return true, nil, apierrors.NewUnauthorized("denied")
					})
				},
			},
			wantErr: "permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLookupEnv := lookupEnv
			t.Cleanup(func() {
				lookupEnv = originalLookupEnv
			})
			lookupEnv = func(key string) (string, bool) {
				switch key {
				case serviceAppEnvKey:
					return "billing", true
				case dominionEnvironmentEnvKey:
					return "dev", true
				case podNamespaceEnvKey:
					return "default", true
				default:
					return "", false
				}
			}

			// given
			clientset := fake.NewSimpleClientset()
			for _, object := range tt.given.endpointSlices {
				if err := clientset.Tracker().Add(object); err != nil {
					t.Fatalf("Tracker().Add() error = %v", err)
				}
			}
			if tt.given.reactor != nil {
				tt.given.reactor(clientset)
			}

			client := &K8sResolver{clientset: clientset}
			target := &Target{App: "billing", Service: "api", Port: tt.given.targetPort}

			// when
			got, err := client.ResolveEndpoints(context.Background(), target, "service-a")

			// then
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ResolveEndpoints() error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveEndpoints() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ResolveEndpoints() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestK8sResolverResolve(t *testing.T) {
	tests := []struct {
		name  string
		given struct {
			objects []runtime.Object
		}
		want    []string
		wantErr string
	}{
		{
			name: "lookup then resolve endpoints",
			given: struct{ objects []runtime.Object }{objects: []runtime.Object{
				serviceObject("service-a", map[string]string{
					ServiceAppLabelKey:                 "billing",
					ServiceComponentLabelKey:           "api",
					ServiceDominionAppLabelKey:         "billing",
					ServiceDominionEnvironmentLabelKey: "dev",
				}),
				endpointSliceObject("service-a", []int32{50051}, []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}}}),
			}},
			want: []string{"10.0.0.1:50051"},
		},
		{
			name:    "lookup error is returned",
			wantErr: "no Services matched selector",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLookupEnv := lookupEnv
			t.Cleanup(func() {
				lookupEnv = originalLookupEnv
			})
			lookupEnv = func(key string) (string, bool) {
				switch key {
				case serviceAppEnvKey:
					return "billing", true
				case dominionEnvironmentEnvKey:
					return "dev", true
				case podNamespaceEnvKey:
					return "default", true
				default:
					return "", false
				}
			}

			// given
			clientset := fake.NewSimpleClientset()
			for _, object := range tt.given.objects {
				if err := clientset.Tracker().Add(object); err != nil {
					t.Fatalf("Tracker().Add() error = %v", err)
				}
			}

			client := &K8sResolver{clientset: clientset}
			target := &Target{App: "billing", Service: "api"}

			// when
			got, err := client.Resolve(context.Background(), target)

			// then
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Resolve() error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Resolve() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func Test_includeEndpoint(t *testing.T) {
	tests := []struct {
		name  string
		given discoveryv1.Endpoint
		want  bool
	}{
		{name: "ready and not terminating", given: discoveryv1.Endpoint{}, want: true},
		{name: "ready false excluded", given: discoveryv1.Endpoint{Conditions: discoveryv1.EndpointConditions{Ready: boolValuePtr(false)}}, want: false},
		{name: "terminating true excluded", given: discoveryv1.Endpoint{Conditions: discoveryv1.EndpointConditions{Terminating: boolValuePtr(true)}}, want: false},
		{name: "ready true and terminating false included", given: discoveryv1.Endpoint{Conditions: discoveryv1.EndpointConditions{Ready: boolValuePtr(true), Terminating: boolValuePtr(false)}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := includeEndpoint(tt.given)

			// then
			if got != tt.want {
				t.Fatalf("includeEndpoint(%#v) = %t, want %t", tt.given, got, tt.want)
			}
		})
	}
}

func serviceObject(name string, labels map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    labels,
		},
	}
}

func endpointSliceObject(serviceName string, ports []int32, endpoints []discoveryv1.Endpoint) *discoveryv1.EndpointSlice {
	endpointPorts := make([]discoveryv1.EndpointPort, 0, len(ports))
	for i := range ports {
		port := ports[i]
		endpointPorts = append(endpointPorts, discoveryv1.EndpointPort{Port: &port})
	}

	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      endpointSliceName(serviceName, ports),
			Namespace: "default",
			Labels: map[string]string{
				discoveryv1.LabelServiceName: serviceName,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports:       endpointPorts,
		Endpoints:   endpoints,
	}
}

func endpointSliceName(serviceName string, ports []int32) string {
	if len(ports) == 0 {
		return serviceName + "-slice"
	}

	return serviceName + "-" + strconv.Itoa(int(ports[0])) + "-slice"
}

func boolValuePtr(value bool) *bool {
	if value {
		return &sharedTrue
	}

	return &sharedFalse
}
