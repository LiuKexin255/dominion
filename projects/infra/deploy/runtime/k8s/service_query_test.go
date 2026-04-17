package k8s

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dominion/projects/infra/deploy/domain"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestK8sRuntime_QueryServiceEndpoints(t *testing.T) {
	ctx := context.Background()

	readyTrue := true
	readyFalse := false
	terminatingTrue := true

	tests := []struct {
		name      string
		namespace string
		envLabel  string
		app       string
		service   string
		seed      func(t *testing.T, fakeClient *kubernetesfake.Clientset)
		wantErr   error
		wantPorts map[string]int32
		wantAddrs []string
	}{
		{
			name:      "service found with ports and endpoints",
			namespace: "test-ns",
			envLabel:  "dev",
			app:       "demo",
			service:   "api",
			seed: func(t *testing.T, fakeClient *kubernetesfake.Clientset) {
				t.Helper()
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "demo-api",
						Namespace: "test-ns",
						Labels: map[string]string{
							"app.kubernetes.io/name":      "demo",
							"app.kubernetes.io/component": "api",
							"dominion.io/environment":     "dev",
						},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{Name: "grpc", Port: 50051},
							{Name: "http", Port: 8080},
						},
					},
				}
				if err := fakeClient.Tracker().Add(svc); err != nil {
					t.Fatalf("seed service: %v", err)
				}

				portNum := int32(50051)
				portNum2 := int32(8080)
				eps := &discoveryv1.EndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "demo-api-abc",
						Namespace: "test-ns",
						Labels: map[string]string{
							"kubernetes.io/service-name": "demo-api",
						},
					},
					Endpoints: []discoveryv1.Endpoint{
						{
							Addresses: []string{"10.0.0.1"},
							Conditions: discoveryv1.EndpointConditions{
								Ready: &readyTrue,
							},
						},
						{
							Addresses: []string{"10.0.0.2"},
							Conditions: discoveryv1.EndpointConditions{
								Ready: &readyTrue,
							},
						},
					},
					Ports: []discoveryv1.EndpointPort{
						{Port: &portNum},
						{Port: &portNum2},
					},
				}
				if err := fakeClient.Tracker().Add(eps); err != nil {
					t.Fatalf("seed endpointslice: %v", err)
				}
			},
			wantPorts: map[string]int32{"grpc": 50051, "http": 8080},
			wantAddrs: []string{
				"10.0.0.1:50051",
				"10.0.0.1:8080",
				"10.0.0.2:50051",
				"10.0.0.2:8080",
			},
		},
		{
			name:      "service not found",
			namespace: "test-ns",
			envLabel:  "dev",
			app:       "demo",
			service:   "api",
			seed: func(t *testing.T, fakeClient *kubernetesfake.Clientset) {
				t.Helper()
			},
			wantErr: domain.ErrServiceNotFound,
		},
		{
			name:      "multiple services conflict",
			namespace: "test-ns",
			envLabel:  "dev",
			app:       "demo",
			service:   "api",
			seed: func(t *testing.T, fakeClient *kubernetesfake.Clientset) {
				t.Helper()
				for _, name := range []string{"svc-a", "svc-b"} {
					svc := &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: "test-ns",
							Labels: map[string]string{
								"app.kubernetes.io/name":      "demo",
								"app.kubernetes.io/component": "api",
								"dominion.io/environment":     "dev",
							},
						},
						Spec: corev1.ServiceSpec{
							Ports: []corev1.ServicePort{
								{Name: "grpc", Port: 50051},
							},
						},
					}
					if err := fakeClient.Tracker().Add(svc); err != nil {
						t.Fatalf("seed service %s: %v", name, err)
					}
				}
			},
			wantErr: errors.New("expected exactly one Service"),
		},
		{
			name:      "service with empty ports",
			namespace: "test-ns",
			envLabel:  "dev",
			app:       "demo",
			service:   "api",
			seed: func(t *testing.T, fakeClient *kubernetesfake.Clientset) {
				t.Helper()
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "demo-api",
						Namespace: "test-ns",
						Labels: map[string]string{
							"app.kubernetes.io/name":      "demo",
							"app.kubernetes.io/component": "api",
							"dominion.io/environment":     "dev",
						},
					},
					Spec: corev1.ServiceSpec{},
				}
				if err := fakeClient.Tracker().Add(svc); err != nil {
					t.Fatalf("seed service: %v", err)
				}
			},
			wantErr: domain.ErrServicePortMapUnavailable,
		},
		{
			name:      "endpoint filtering excludes not-ready and terminating",
			namespace: "test-ns",
			envLabel:  "dev",
			app:       "demo",
			service:   "api",
			seed: func(t *testing.T, fakeClient *kubernetesfake.Clientset) {
				t.Helper()
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "demo-api",
						Namespace: "test-ns",
						Labels: map[string]string{
							"app.kubernetes.io/name":      "demo",
							"app.kubernetes.io/component": "api",
							"dominion.io/environment":     "dev",
						},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{Name: "grpc", Port: 50051},
						},
					},
				}
				if err := fakeClient.Tracker().Add(svc); err != nil {
					t.Fatalf("seed service: %v", err)
				}

				portNum := int32(50051)
				eps := &discoveryv1.EndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "demo-api-xyz",
						Namespace: "test-ns",
						Labels: map[string]string{
							"kubernetes.io/service-name": "demo-api",
						},
					},
					Endpoints: []discoveryv1.Endpoint{
						{
							Addresses: []string{"10.0.0.1"},
							Conditions: discoveryv1.EndpointConditions{
								Ready: &readyTrue,
							},
						},
						{
							Addresses: []string{"10.0.0.2"},
							Conditions: discoveryv1.EndpointConditions{
								Ready: &readyFalse,
							},
						},
						{
							Addresses: []string{"10.0.0.3"},
							Conditions: discoveryv1.EndpointConditions{
								Ready:       &readyTrue,
								Terminating: &terminatingTrue,
							},
						},
					},
					Ports: []discoveryv1.EndpointPort{
						{Port: &portNum},
					},
				}
				if err := fakeClient.Tracker().Add(eps); err != nil {
					t.Fatalf("seed endpointslice: %v", err)
				}
			},
			wantPorts: map[string]int32{"grpc": 50051},
			wantAddrs: []string{"10.0.0.1:50051"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := kubernetesfake.NewSimpleClientset()
			tt.seed(t, fakeClient)

			runtime := NewK8sRuntime(&RuntimeClient{
				TypedClient: fakeClient,
				K8sConfig: &K8sConfig{
					Namespace: tt.namespace,
				},
			})

			result, err := runtime.QueryServiceEndpoints(ctx, tt.envLabel, tt.app, tt.service)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr.Error())
				}
				if !errors.Is(err, tt.wantErr) && !errors.Is(err, domain.ErrServiceNotFound) && !errors.Is(err, domain.ErrServicePortMapUnavailable) {
					if !strings.Contains(err.Error(), tt.wantErr.Error()) {
						t.Fatalf("expected error containing %q, got %q", tt.wantErr.Error(), err.Error())
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if len(result.Ports) != len(tt.wantPorts) {
				t.Fatalf("ports count = %d, want %d", len(result.Ports), len(tt.wantPorts))
			}
			for k, v := range tt.wantPorts {
				if result.Ports[k] != v {
					t.Fatalf("ports[%q] = %d, want %d", k, result.Ports[k], v)
				}
			}

			if len(result.Endpoints) != len(tt.wantAddrs) {
				t.Fatalf("endpoints count = %d, want %d: got %v", len(result.Endpoints), len(tt.wantAddrs), result.Endpoints)
			}
			for i, addr := range tt.wantAddrs {
				if result.Endpoints[i] != addr {
					t.Fatalf("endpoints[%d] = %q, want %q", i, result.Endpoints[i], addr)
				}
			}
		})
	}
}
