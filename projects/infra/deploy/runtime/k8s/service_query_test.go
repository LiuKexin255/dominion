package k8s

import (
	"context"
	"errors"
	"reflect"
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
		want      *domain.ServiceQueryResult
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
						ClusterIP: "10.96.0.10",
						Ports:     []corev1.ServicePort{{Name: "grpc", Port: 50051}, {Name: "http", Port: 8080}},
					},
				}
				if err := fakeClient.Tracker().Add(svc); err != nil {
					t.Fatalf("seed service: %v", err)
				}

				eps := &discoveryv1.EndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "demo-api-abc",
						Namespace: "test-ns",
						Labels:    map[string]string{"kubernetes.io/service-name": "demo-api"},
					},
					Endpoints: []discoveryv1.Endpoint{
						{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyTrue}},
						{Addresses: []string{"10.0.0.2"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyTrue}},
					},
				}
				if err := fakeClient.Tracker().Add(eps); err != nil {
					t.Fatalf("seed endpointslice: %v", err)
				}
			},
			want: &domain.ServiceQueryResult{
				Ports:      map[string]int32{"grpc": 50051, "http": 8080},
				Endpoints:  []string{"10.0.0.1:50051", "10.0.0.1:8080", "10.0.0.2:50051", "10.0.0.2:8080"},
				IsStateful: false,
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
						Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.20", Ports: []corev1.ServicePort{{Name: "grpc", Port: 50051}}},
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
					Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.30"},
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
					Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.40", Ports: []corev1.ServicePort{{Name: "grpc", Port: 50051}}},
				}
				if err := fakeClient.Tracker().Add(svc); err != nil {
					t.Fatalf("seed service: %v", err)
				}

				eps := &discoveryv1.EndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "demo-api-xyz",
						Namespace: "test-ns",
						Labels:    map[string]string{"kubernetes.io/service-name": "demo-api"},
					},
					Endpoints: []discoveryv1.Endpoint{
						{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyTrue}},
						{Addresses: []string{"10.0.0.2"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyFalse}},
						{Addresses: []string{"10.0.0.3"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyTrue, Terminating: &terminatingTrue}},
					},
				}
				if err := fakeClient.Tracker().Add(eps); err != nil {
					t.Fatalf("seed endpointslice: %v", err)
				}
			},
			want: &domain.ServiceQueryResult{
				Ports:      map[string]int32{"grpc": 50051},
				Endpoints:  []string{"10.0.0.1:50051"},
				IsStateful: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := kubernetesfake.NewSimpleClientset()
			tt.seed(t, fakeClient)

			runtime := NewK8sRuntime(&RuntimeClient{TypedClient: fakeClient, K8sConfig: &K8sConfig{Namespace: tt.namespace}})
			result, err := runtime.QueryServiceEndpoints(ctx, tt.envLabel, tt.app, tt.service)

			assertQueryResult(t, result, err, tt.want, tt.wantErr)
		})
	}
}

func TestK8sRuntime_QueryStatefulServiceEndpoints(t *testing.T) {
	ctx := context.Background()

	readyTrue := true
	readyFalse := false

	tests := []struct {
		name      string
		namespace string
		envLabel  string
		app       string
		service   string
		seed      func(t *testing.T, fakeClient *kubernetesfake.Clientset)
		wantErr   error
		want      *domain.ServiceQueryResult
	}{
		{
			name:      "stateful service with instances",
			namespace: "test-ns",
			envLabel:  "dev",
			app:       "demo",
			service:   "api",
			seed: func(t *testing.T, fakeClient *kubernetesfake.Clientset) {
				t.Helper()
				labels := map[string]string{
					"app.kubernetes.io/name":      "demo",
					"app.kubernetes.io/component": "api",
					"dominion.io/environment":     "dev",
				}
				for _, svc := range []*corev1.Service{
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api", Namespace: "test-ns", Labels: labels}, Spec: corev1.ServiceSpec{ClusterIP: corev1.ClusterIPNone, Ports: []corev1.ServicePort{{Name: "grpc", Port: 50051}}}},
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api-2", Namespace: "test-ns", Labels: labels}, Spec: corev1.ServiceSpec{ClusterIP: "10.96.1.12", Selector: map[string]string{statefulSetPodNameLabelKey: "demo-api-2"}, Ports: []corev1.ServicePort{{Name: "grpc", Port: 50051}}}},
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api-0", Namespace: "test-ns", Labels: labels}, Spec: corev1.ServiceSpec{ClusterIP: "10.96.1.10", Selector: map[string]string{statefulSetPodNameLabelKey: "demo-api-0"}, Ports: []corev1.ServicePort{{Name: "grpc", Port: 50051}}}},
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api-1", Namespace: "test-ns", Labels: labels}, Spec: corev1.ServiceSpec{ClusterIP: "10.96.1.11", Selector: map[string]string{statefulSetPodNameLabelKey: "demo-api-1"}, Ports: []corev1.ServicePort{{Name: "grpc", Port: 50051}}}},
				} {
					if err := fakeClient.Tracker().Add(svc); err != nil {
						t.Fatalf("seed service %s: %v", svc.Name, err)
					}
				}

				for _, slice := range []*discoveryv1.EndpointSlice{
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api-agg", Namespace: "test-ns", Labels: map[string]string{"kubernetes.io/service-name": "demo-api"}}, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.10"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyTrue}}, {Addresses: []string{"10.0.0.11"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyTrue}}, {Addresses: []string{"10.0.0.12"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyTrue}}}},
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api-0-slice", Namespace: "test-ns", Labels: map[string]string{"kubernetes.io/service-name": "demo-api-0"}}, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.10"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyTrue}}}},
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api-1-slice", Namespace: "test-ns", Labels: map[string]string{"kubernetes.io/service-name": "demo-api-1"}}, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.11"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyTrue}}}},
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api-2-slice", Namespace: "test-ns", Labels: map[string]string{"kubernetes.io/service-name": "demo-api-2"}}, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.12"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyTrue}}}},
				} {
					if err := fakeClient.Tracker().Add(slice); err != nil {
						t.Fatalf("seed endpointslice %s: %v", slice.Name, err)
					}
				}
			},
			want: &domain.ServiceQueryResult{
				Ports:             map[string]int32{"grpc": 50051},
				Endpoints:         []string{"10.0.0.10:50051", "10.0.0.11:50051", "10.0.0.12:50051"},
				IsStateful:        true,
				StatefulInstances: []*domain.StatefulInstance{{Index: 0, Hostname: "demo-api-0", Endpoints: []string{"10.0.0.10:50051"}}, {Index: 1, Hostname: "demo-api-1", Endpoints: []string{"10.0.0.11:50051"}}, {Index: 2, Hostname: "demo-api-2", Endpoints: []string{"10.0.0.12:50051"}}},
			},
		},
		{
			name:      "stateful service instance without ready endpoints returns empty slice",
			namespace: "test-ns",
			envLabel:  "dev",
			app:       "demo",
			service:   "api",
			seed: func(t *testing.T, fakeClient *kubernetesfake.Clientset) {
				t.Helper()
				labels := map[string]string{
					"app.kubernetes.io/name":      "demo",
					"app.kubernetes.io/component": "api",
					"dominion.io/environment":     "dev",
				}
				for _, svc := range []*corev1.Service{
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api", Namespace: "test-ns", Labels: labels}, Spec: corev1.ServiceSpec{ClusterIP: corev1.ClusterIPNone, Ports: []corev1.ServicePort{{Name: "grpc", Port: 50051}}}},
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api-0", Namespace: "test-ns", Labels: labels}, Spec: corev1.ServiceSpec{ClusterIP: "10.96.2.10", Selector: map[string]string{statefulSetPodNameLabelKey: "demo-api-0"}, Ports: []corev1.ServicePort{{Name: "grpc", Port: 50051}}}},
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api-1", Namespace: "test-ns", Labels: labels}, Spec: corev1.ServiceSpec{ClusterIP: "10.96.2.11", Selector: map[string]string{statefulSetPodNameLabelKey: "demo-api-1"}, Ports: []corev1.ServicePort{{Name: "grpc", Port: 50051}}}},
				} {
					if err := fakeClient.Tracker().Add(svc); err != nil {
						t.Fatalf("seed service %s: %v", svc.Name, err)
					}
				}

				for _, slice := range []*discoveryv1.EndpointSlice{
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api-agg", Namespace: "test-ns", Labels: map[string]string{"kubernetes.io/service-name": "demo-api"}}, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.10"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyTrue}}}},
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api-0-slice", Namespace: "test-ns", Labels: map[string]string{"kubernetes.io/service-name": "demo-api-0"}}, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.10"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyTrue}}}},
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api-1-slice", Namespace: "test-ns", Labels: map[string]string{"kubernetes.io/service-name": "demo-api-1"}}, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.11"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyFalse}}}},
				} {
					if err := fakeClient.Tracker().Add(slice); err != nil {
						t.Fatalf("seed endpointslice %s: %v", slice.Name, err)
					}
				}
			},
			want: &domain.ServiceQueryResult{
				Ports:             map[string]int32{"grpc": 50051},
				Endpoints:         []string{"10.0.0.10:50051"},
				IsStateful:        true,
				StatefulInstances: []*domain.StatefulInstance{{Index: 0, Hostname: "demo-api-0", Endpoints: []string{"10.0.0.10:50051"}}, {Index: 1, Hostname: "demo-api-1", Endpoints: nil}},
			},
		},
		{
			name:      "multiple services without headless remain conflict",
			namespace: "test-ns",
			envLabel:  "dev",
			app:       "demo",
			service:   "api",
			seed: func(t *testing.T, fakeClient *kubernetesfake.Clientset) {
				t.Helper()
				for _, svc := range []*corev1.Service{
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api-0", Namespace: "test-ns", Labels: map[string]string{"app.kubernetes.io/name": "demo", "app.kubernetes.io/component": "api", "dominion.io/environment": "dev"}}, Spec: corev1.ServiceSpec{ClusterIP: "10.96.3.10", Ports: []corev1.ServicePort{{Name: "grpc", Port: 50051}}}},
					{ObjectMeta: metav1.ObjectMeta{Name: "demo-api-1", Namespace: "test-ns", Labels: map[string]string{"app.kubernetes.io/name": "demo", "app.kubernetes.io/component": "api", "dominion.io/environment": "dev"}}, Spec: corev1.ServiceSpec{ClusterIP: "10.96.3.11", Ports: []corev1.ServicePort{{Name: "grpc", Port: 50051}}}},
				} {
					if err := fakeClient.Tracker().Add(svc); err != nil {
						t.Fatalf("seed service %s: %v", svc.Name, err)
					}
				}
			},
			wantErr: errors.New("expected exactly one governing Service"),
		},
		{
			name:      "stateful service with zero instances",
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
					Spec: corev1.ServiceSpec{ClusterIP: corev1.ClusterIPNone, Ports: []corev1.ServicePort{{Name: "grpc", Port: 50051}}},
				}
				if err := fakeClient.Tracker().Add(svc); err != nil {
					t.Fatalf("seed service: %v", err)
				}

				eps := &discoveryv1.EndpointSlice{
					ObjectMeta: metav1.ObjectMeta{Name: "demo-api-agg", Namespace: "test-ns", Labels: map[string]string{"kubernetes.io/service-name": "demo-api"}},
					Endpoints:  []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.10"}, Conditions: discoveryv1.EndpointConditions{Ready: &readyTrue}}},
				}
				if err := fakeClient.Tracker().Add(eps); err != nil {
					t.Fatalf("seed endpointslice: %v", err)
				}
			},
			want: &domain.ServiceQueryResult{
				Ports:      map[string]int32{"grpc": 50051},
				Endpoints:  []string{"10.0.0.10:50051"},
				IsStateful: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := kubernetesfake.NewSimpleClientset()
			tt.seed(t, fakeClient)

			runtime := NewK8sRuntime(&RuntimeClient{TypedClient: fakeClient, K8sConfig: &K8sConfig{Namespace: tt.namespace}})
			result, err := runtime.QueryStatefulServiceEndpoints(ctx, tt.envLabel, tt.app, tt.service)

			assertQueryResult(t, result, err, tt.want, tt.wantErr)
		})
	}
}

func assertQueryResult(t *testing.T, result *domain.ServiceQueryResult, err error, want *domain.ServiceQueryResult, wantErr error) {
	t.Helper()

	if wantErr != nil {
		if err == nil {
			t.Fatalf("expected error containing %q, got nil", wantErr.Error())
		}
		if !errors.Is(err, wantErr) && !strings.Contains(err.Error(), wantErr.Error()) {
			t.Fatalf("expected error containing %q, got %q", wantErr.Error(), err.Error())
		}
		return
	}

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("result = %#v, want %#v", result, want)
	}
}
