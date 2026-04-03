package k8s

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
)

type executorDeleteFunc func(context.Context, string, string) error

func TestExecutor_Delete_RemovesOnlyManagedResources(t *testing.T) {
	tests := []struct {
		name        string
		app         string
		environment string
		given       func(*testing.T, *FakeHarness)
		then        func(*testing.T, *FakeHarness)
	}{
		{
			name:        "delete only resources matching managed app and environment labels",
			app:         "grpc-hello-world",
			environment: "dev",
			given: func(t *testing.T, h *FakeHarness) {
				seedManagedDeleteResources(t, h, "grpc-hello-world", "dev")
				seedDeleteResources(t, h, deleteSeedOptions{
					app:         "grpc-hello-world",
					environment: "dev",
					managedBy:   "someone-else",
					suffix:      "unmanaged",
				})
				seedDeleteResources(t, h, deleteSeedOptions{
					app:         "grpc-hello-world",
					environment: "prod",
					managedBy:   h.RuntimeClient().K8sConfig.ManagedBy,
					suffix:      "other-env",
				})
				seedDeleteResources(t, h, deleteSeedOptions{
					app:         "billing",
					environment: "dev",
					managedBy:   h.RuntimeClient().K8sConfig.ManagedBy,
					suffix:      "other-app",
				})
			},
			then: func(t *testing.T, h *FakeHarness) {
				namespace := h.RuntimeClient().K8sConfig.Namespace
				h.AssertDeploymentDeleted(namespace, "deploy-grpc-hello-world-gateway-dev")
				h.AssertServiceDeleted(namespace, "svc-grpc-hello-world-gateway-dev")
				h.AssertHTTPRouteDeleted(namespace, "route-grpc-hello-world-gateway-dev")

				h.AssertDeploymentCreated(namespace, "deploy-grpc-hello-world-gateway-dev-unmanaged")
				h.AssertServiceCreated(namespace, "svc-grpc-hello-world-gateway-dev-unmanaged")
				h.AssertHTTPRouteCreated(namespace, "route-grpc-hello-world-gateway-dev-unmanaged")

				h.AssertDeploymentCreated(namespace, "deploy-grpc-hello-world-gateway-prod-other-env")
				h.AssertServiceCreated(namespace, "svc-grpc-hello-world-gateway-prod-other-env")
				h.AssertHTTPRouteCreated(namespace, "route-grpc-hello-world-gateway-prod-other-env")

				h.AssertDeploymentCreated(namespace, "deploy-billing-gateway-dev-other-app")
				h.AssertServiceCreated(namespace, "svc-billing-gateway-dev-other-app")
				h.AssertHTTPRouteCreated(namespace, "route-billing-gateway-dev-other-app")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			h := NewFakeHarness(t)
			tt.given(t, h)
			executor := NewExecutor(h.RuntimeClient())
			deleteFunc := requireExecutorDelete(t, executor)

			// when
			err := deleteFunc(context.Background(), tt.app, tt.environment)
			if err != nil {
				t.Fatalf("Delete() failed: %v", err)
			}

			// then
			tt.then(t, h)
		})
	}
}

func TestExecutor_Delete_IsIdempotent(t *testing.T) {
	tests := []struct {
		name  string
		given func(*testing.T, *FakeHarness)
		then  func(*testing.T, *FakeHarness)
	}{
		{
			name: "second delete succeeds after resources already removed",
			given: func(t *testing.T, h *FakeHarness) {
				seedManagedDeleteResources(t, h, "grpc-hello-world", "dev")
			},
			then: func(t *testing.T, h *FakeHarness) {
				namespace := h.RuntimeClient().K8sConfig.Namespace
				h.AssertDeploymentDeleted(namespace, "deploy-grpc-hello-world-gateway-dev")
				h.AssertServiceDeleted(namespace, "svc-grpc-hello-world-gateway-dev")
				h.AssertHTTPRouteDeleted(namespace, "route-grpc-hello-world-gateway-dev")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			h := NewFakeHarness(t)
			tt.given(t, h)
			executor := NewExecutor(h.RuntimeClient())
			deleteFunc := requireExecutorDelete(t, executor)

			// when
			if err := deleteFunc(context.Background(), "grpc-hello-world", "dev"); err != nil {
				t.Fatalf("first Delete() failed: %v", err)
			}
			if err := deleteFunc(context.Background(), "grpc-hello-world", "dev"); err != nil {
				t.Fatalf("second Delete() failed: %v", err)
			}

			// then
			tt.then(t, h)
		})
	}
}

func TestExecutor_Delete_NotFoundIsSuccess(t *testing.T) {
	tests := []struct {
		name string
		app  string
		env  string
	}{
		{
			name: "missing managed resources return nil",
			app:  "grpc-hello-world",
			env:  "dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			h := NewFakeHarness(t)
			executor := NewExecutor(h.RuntimeClient())
			deleteFunc := requireExecutorDelete(t, executor)

			// when
			err := deleteFunc(context.Background(), tt.app, tt.env)
			if err != nil {
				t.Fatalf("Delete() failed: %v", err)
			}
		})
	}
}

func TestExecutor_Delete_ReverseOrder(t *testing.T) {
	tests := []struct {
		name      string
		wantOrder []string
	}{
		{
			name:      "delete httproutes before services before deployments",
			wantOrder: []string{resourceKindHTTPRoute, resourceKindService, resourceKindDeployment},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			h := NewFakeHarness(t)
			seedManagedDeleteResources(t, h, "grpc-hello-world", "dev")
			executor := NewExecutor(h.RuntimeClient())
			deleteFunc := requireExecutorDelete(t, executor)

			var deleteOrder []string
			recordDelete := func(action k8stesting.Action) {
				resourceType, ok := normalizeResourceTypeFromAction(action.GetResource().Resource)
				if ok {
					deleteOrder = append(deleteOrder, resourceType)
				}
			}
			h.typedClient.PrependReactor("delete", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
				recordDelete(action)
				return false, nil, nil
			})
			h.typedClient.PrependReactor("delete-collection", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
				recordDelete(action)
				return false, nil, nil
			})
			h.dynamicClient.PrependReactor("delete", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
				recordDelete(action)
				return false, nil, nil
			})
			h.dynamicClient.PrependReactor("delete-collection", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
				recordDelete(action)
				return false, nil, nil
			})

			// when
			err := deleteFunc(context.Background(), "grpc-hello-world", "dev")
			if err != nil {
				t.Fatalf("Delete() failed: %v", err)
			}

			// then
			if !reflect.DeepEqual(deleteOrder, tt.wantOrder) {
				t.Fatalf("delete order = %v, want %v", deleteOrder, tt.wantOrder)
			}
		})
	}
}

type deleteSeedOptions struct {
	app         string
	environment string
	managedBy   string
	suffix      string
}

func requireExecutorDelete(t *testing.T, executor *Executor) executorDeleteFunc {
	t.Helper()

	type deleteExecutor interface {
		Delete(context.Context, string, string) error
	}

	deleter, ok := any(executor).(deleteExecutor)
	if !ok {
		t.Fatal("Executor.Delete() is not implemented yet")
	}

	return deleter.Delete
}

func seedManagedDeleteResources(t *testing.T, h *FakeHarness, app string, environment string) {
	t.Helper()

	seedDeleteResources(t, h, deleteSeedOptions{
		app:         app,
		environment: environment,
		managedBy:   h.RuntimeClient().K8sConfig.ManagedBy,
	})
}

func seedDeleteResources(t *testing.T, h *FakeHarness, options deleteSeedOptions) {
	t.Helper()

	k8sConfig := h.RuntimeClient().K8sConfig
	labels := map[string]string{
		managedByLabelKey:   options.managedBy,
		appLabelKey:         options.app,
		environmentLabelKey: options.environment,
	}

	deploymentName := fmt.Sprintf("deploy-%s-gateway-%s", options.app, options.environment)
	serviceName := fmt.Sprintf("svc-%s-gateway-%s", options.app, options.environment)
	routeName := fmt.Sprintf("route-%s-gateway-%s", options.app, options.environment)
	if options.suffix != "" {
		deploymentName = deploymentName + "-" + options.suffix
		serviceName = serviceName + "-" + options.suffix
		routeName = routeName + "-" + options.suffix
	}

	h.SeedDeployment(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: k8sConfig.Namespace,
			Labels:    cloneStringMap(labels),
		},
	})
	h.SeedService(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: k8sConfig.Namespace,
			Labels:    cloneStringMap(labels),
		},
	})
	h.SeedHTTPRoute(&unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       httpRouteKind,
		"metadata": map[string]any{
			"name":      routeName,
			"namespace": k8sConfig.Namespace,
			"labels":    cloneAnyMap(labels),
		},
	}})
}

func cloneStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	maps.Copy(result, source)

	return result
}

func cloneAnyMap(source map[string]string) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}

	return result
}
