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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
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
			want: "app.kubernetes.io/component=service-a,app.kubernetes.io/name=app-a,dominion.io/environment=dev",
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
			want: "app.kubernetes.io/component=service-b,app.kubernetes.io/name=app-b,dominion.io/environment=prod",
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
			k8sResolver, ok := got.(*K8sResolver)
			if !ok {
				t.Fatalf("NewK8sResolver() type = %T, want *K8sResolver", got)
			}
			if k8sResolver.clientset == nil {
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
			name: "resolves service endpoints",
			given: struct{ objects []runtime.Object }{objects: []runtime.Object{
				serviceObject("service-a", map[string]string{
					ServiceAppLabelKey:                 "billing",
					ServiceComponentLabelKey:           "api",
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
			target := &Target{App: "billing", Service: "api", PortSelector: NumericPort(50051)}

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

func Test_endpointSlicePorts(t *testing.T) {
	httpName := "http"
	metricsName := "metrics"
	httpPort := int32(8080)
	metricsPort := int32(9090)
	defaultPort := int32(7070)
	endpointSlice := endpointSliceWithPorts([]discoveryv1.EndpointPort{
		{Name: &httpName, Port: &httpPort},
		{Name: &metricsName, Port: &metricsPort},
		{Port: &defaultPort},
	})

	tests := []struct {
		name         string
		portSelector PortSelector
		want         []int
	}{
		{name: "numeric port selected directly", portSelector: NumericPort(7070), want: []int{7070}},
		{name: "named port found", portSelector: NamedPort("metrics"), want: []int{9090}},
		{name: "named port not found", portSelector: NamedPort("admin"), want: nil},
		{name: "empty selector returns all ports", portSelector: PortSelector{}, want: []int{8080, 9090, 7070}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := endpointSlicePorts(endpointSlice, tt.portSelector)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("endpointSlicePorts() = %#v, want %#v", got, tt.want)
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
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      endpointSliceName(serviceName, ports),
			Namespace: "default",
			Labels: map[string]string{
				discoveryv1.LabelServiceName: serviceName,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports:       endpointSlicePortsFromInts(ports),
		Endpoints:   endpoints,
	}
}

func endpointSliceWithPorts(ports []discoveryv1.EndpointPort) discoveryv1.EndpointSlice {
	return discoveryv1.EndpointSlice{Ports: ports}
}

func endpointSlicePortsFromInts(ports []int32) []discoveryv1.EndpointPort {
	endpointPorts := make([]discoveryv1.EndpointPort, 0, len(ports))
	for i := range ports {
		port := ports[i]
		endpointPorts = append(endpointPorts, discoveryv1.EndpointPort{Port: &port})
	}

	return endpointPorts
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
