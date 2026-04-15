package k8s

import (
	"context"
	"maps"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
)

type executorDeleteFunc func(context.Context, string) error

func TestExecutor_Delete_RemovesOnlyManagedResources(t *testing.T) {
	tests := []struct {
		name        string
		app         string
		fullEnvName string
		given       func(*testing.T, *FakeHarness)
		then        func(*testing.T, *FakeHarness)
	}{
		{
			name:        "delete only resources matching managed app and environment labels",
			app:         "grpc-hello-world",
			fullEnvName: "grpc-hello-world/dev",
			given: func(t *testing.T, h *FakeHarness) {
				seedManagedDeleteResources(t, h, "grpc-hello-world", "grpc-hello-world/dev")
				seedDeleteResources(t, h, deleteSeedOptions{
					app:         "grpc-hello-world",
					fullEnvName: "grpc-hello-world/dev",
					managedBy:   "someone-else",
					suffix:      "unmanaged",
				})
				seedDeleteResources(t, h, deleteSeedOptions{
					app:         "grpc-hello-world",
					fullEnvName: "grpc-hello-world/prod",
					managedBy:   h.RuntimeClient().K8sConfig.ManagedBy,
					suffix:      "other-env",
				})
				seedDeleteResources(t, h, deleteSeedOptions{
					app:         "billing",
					fullEnvName: "billing/dev",
					managedBy:   h.RuntimeClient().K8sConfig.ManagedBy,
					suffix:      "other-app",
				})
			},
			then: func(t *testing.T, h *FakeHarness) {
				namespace := h.RuntimeClient().K8sConfig.Namespace
				h.AssertDeploymentDeleted(namespace, deleteResourceName(WorkloadKindDeployment, "grpc-hello-world/dev", "grpc-hello-world", ""))
				h.AssertServiceDeleted(namespace, deleteResourceName(WorkloadKindService, "grpc-hello-world/dev", "grpc-hello-world", ""))
				h.AssertHTTPRouteDeleted(namespace, deleteResourceName(WorkloadKindHTTPRoute, "grpc-hello-world/dev", "grpc-hello-world", ""))

				h.AssertDeploymentCreated(namespace, deleteResourceName(WorkloadKindDeployment, "grpc-hello-world/dev", "grpc-hello-world", "unmanaged"))
				h.AssertServiceCreated(namespace, deleteResourceName(WorkloadKindService, "grpc-hello-world/dev", "grpc-hello-world", "unmanaged"))
				h.AssertHTTPRouteCreated(namespace, deleteResourceName(WorkloadKindHTTPRoute, "grpc-hello-world/dev", "grpc-hello-world", "unmanaged"))

				h.AssertDeploymentCreated(namespace, deleteResourceName(WorkloadKindDeployment, "grpc-hello-world/prod", "grpc-hello-world", "other-env"))
				h.AssertServiceCreated(namespace, deleteResourceName(WorkloadKindService, "grpc-hello-world/prod", "grpc-hello-world", "other-env"))
				h.AssertHTTPRouteCreated(namespace, deleteResourceName(WorkloadKindHTTPRoute, "grpc-hello-world/prod", "grpc-hello-world", "other-env"))

				h.AssertDeploymentCreated(namespace, deleteResourceName(WorkloadKindDeployment, "billing/dev", "billing", "other-app"))
				h.AssertServiceCreated(namespace, deleteResourceName(WorkloadKindService, "billing/dev", "billing", "other-app"))
				h.AssertHTTPRouteCreated(namespace, deleteResourceName(WorkloadKindHTTPRoute, "billing/dev", "billing", "other-app"))
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
			err := deleteFunc(context.Background(), tt.fullEnvName)
			if err != nil {
				t.Fatalf("Delete() failed: %v", err)
			}

			// then
			tt.then(t, h)
		})
	}
}

func TestExecutor_Delete_OldLabeledResourcesAreNotDeleted(t *testing.T) {
	// given
	h := NewFakeHarness(t)
	seedOldDeleteResources(t, h, deleteSeedOptions{
		app:         "grpc-hello-world",
		fullEnvName: "grpc-hello-world/dev",
		managedBy:   h.RuntimeClient().K8sConfig.ManagedBy,
		suffix:      "legacy",
	})
	executor := NewExecutor(h.RuntimeClient())
	deleteFunc := requireExecutorDelete(t, executor)

	// when
	if err := deleteFunc(context.Background(), "grpc-hello-world/dev"); err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}

	// then
	namespace := h.RuntimeClient().K8sConfig.Namespace
	h.AssertDeploymentCreated(namespace, deleteResourceName(WorkloadKindDeployment, "grpc-hello-world/dev", "grpc-hello-world", "legacy"))
	h.AssertServiceCreated(namespace, deleteResourceName(WorkloadKindService, "grpc-hello-world/dev", "grpc-hello-world", "legacy"))
	h.AssertHTTPRouteCreated(namespace, deleteResourceName(WorkloadKindHTTPRoute, "grpc-hello-world/dev", "grpc-hello-world", "legacy"))
}

func TestExecutor_Delete_MixedResourcesScopedCorrectly(t *testing.T) {
	// given
	h := NewFakeHarness(t)
	seedManagedDeleteResources(t, h, "grpc-hello-world", "grpc-hello-world/dev")
	seedDeleteResources(t, h, deleteSeedOptions{
		app:         "grpc-hello-world",
		fullEnvName: "grpc-hello-world/prod",
		managedBy:   h.RuntimeClient().K8sConfig.ManagedBy,
		suffix:      "other-env",
	})
	seedDeleteResources(t, h, deleteSeedOptions{
		app:         "billing",
		fullEnvName: "billing/dev",
		managedBy:   h.RuntimeClient().K8sConfig.ManagedBy,
		suffix:      "other-app",
	})
	executor := NewExecutor(h.RuntimeClient())
	deleteFunc := requireExecutorDelete(t, executor)

	// when
	if err := deleteFunc(context.Background(), "grpc-hello-world/dev"); err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}

	// then
	namespace := h.RuntimeClient().K8sConfig.Namespace
	h.AssertDeploymentDeleted(namespace, deleteResourceName(WorkloadKindDeployment, "grpc-hello-world/dev", "grpc-hello-world", ""))
	h.AssertServiceDeleted(namespace, deleteResourceName(WorkloadKindService, "grpc-hello-world/dev", "grpc-hello-world", ""))
	h.AssertHTTPRouteDeleted(namespace, deleteResourceName(WorkloadKindHTTPRoute, "grpc-hello-world/dev", "grpc-hello-world", ""))

	h.AssertDeploymentCreated(namespace, deleteResourceName(WorkloadKindDeployment, "grpc-hello-world/prod", "grpc-hello-world", "other-env"))
	h.AssertServiceCreated(namespace, deleteResourceName(WorkloadKindService, "grpc-hello-world/prod", "grpc-hello-world", "other-env"))
	h.AssertHTTPRouteCreated(namespace, deleteResourceName(WorkloadKindHTTPRoute, "grpc-hello-world/prod", "grpc-hello-world", "other-env"))

	h.AssertDeploymentCreated(namespace, deleteResourceName(WorkloadKindDeployment, "billing/dev", "billing", "other-app"))
	h.AssertServiceCreated(namespace, deleteResourceName(WorkloadKindService, "billing/dev", "billing", "other-app"))
	h.AssertHTTPRouteCreated(namespace, deleteResourceName(WorkloadKindHTTPRoute, "billing/dev", "billing", "other-app"))
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
				seedManagedDeleteResources(t, h, "grpc-hello-world", "grpc-hello-world/dev")
			},
			then: func(t *testing.T, h *FakeHarness) {
				namespace := h.RuntimeClient().K8sConfig.Namespace
				h.AssertDeploymentDeleted(namespace, deleteResourceName(WorkloadKindDeployment, "grpc-hello-world/dev", "grpc-hello-world", ""))
				h.AssertServiceDeleted(namespace, deleteResourceName(WorkloadKindService, "grpc-hello-world/dev", "grpc-hello-world", ""))
				h.AssertHTTPRouteDeleted(namespace, deleteResourceName(WorkloadKindHTTPRoute, "grpc-hello-world/dev", "grpc-hello-world", ""))
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
			if err := deleteFunc(context.Background(), "grpc-hello-world/dev"); err != nil {
				t.Fatalf("first Delete() failed: %v", err)
			}
			if err := deleteFunc(context.Background(), "grpc-hello-world/dev"); err != nil {
				t.Fatalf("second Delete() failed: %v", err)
			}

			// then
			tt.then(t, h)
		})
	}
}

func TestExecutor_Delete_NotFoundIsSuccess(t *testing.T) {
	tests := []struct {
		name        string
		app         string
		fullEnvName string
	}{
		{
			name:        "missing managed resources return nil",
			app:         "grpc-hello-world",
			fullEnvName: "grpc-hello-world/dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			h := NewFakeHarness(t)
			executor := NewExecutor(h.RuntimeClient())
			deleteFunc := requireExecutorDelete(t, executor)

			// when
			err := deleteFunc(context.Background(), tt.fullEnvName)
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
			seedManagedDeleteResources(t, h, "grpc-hello-world", "grpc-hello-world/dev")
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
			err := deleteFunc(context.Background(), "grpc-hello-world/dev")
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

func TestExecutor_Delete_MongoDB_RemovesWorkloadAndSecretPreservesPVC(t *testing.T) {
	// given
	stubLoadK8sConfig(t, newTestK8sConfigWithMongoProfile())
	h := NewFakeHarness(t)
	workload := newTestMongoDBWorkload()
	pvc, err := BuildMongoDBPVC(workload)
	if err != nil {
		t.Fatalf("BuildMongoDBPVC() failed: %v", err)
	}
	secret, err := BuildMongoDBSecret(workload)
	if err != nil {
		t.Fatalf("BuildMongoDBSecret() failed: %v", err)
	}
	deployment, err := BuildMongoDBDeployment(workload)
	if err != nil {
		t.Fatalf("BuildMongoDBDeployment() failed: %v", err)
	}
	service, err := BuildMongoDBService(workload)
	if err != nil {
		t.Fatalf("BuildMongoDBService() failed: %v", err)
	}
	h.SeedPVC(pvc)
	h.SeedSecret(secret)
	h.SeedDeployment(deployment)
	h.SeedService(service)
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

	// when
	err = deleteFunc(context.Background(), workload.EnvironmentName)
	if err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}

	// then
	wantOrder := []string{resourceKindService, resourceKindDeployment, resourceKindSecret}
	if !reflect.DeepEqual(deleteOrder, wantOrder) {
		t.Fatalf("delete order = %v, want %v", deleteOrder, wantOrder)
	}
	h.AssertServiceDeleted(service.Namespace, service.Name)
	h.AssertDeploymentDeleted(deployment.Namespace, deployment.Name)
	h.AssertSecretDeleted(secret.Namespace, secret.Name)
	h.AssertPVCCreated(pvc.Namespace, pvc.Name)
}

type deleteSeedOptions struct {
	app         string
	fullEnvName string
	managedBy   string
	suffix      string
}

func requireExecutorDelete(t *testing.T, executor *Executor) executorDeleteFunc {
	t.Helper()

	type deleteExecutor interface {
		Delete(context.Context, string) error
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
		fullEnvName: environment,
		managedBy:   h.RuntimeClient().K8sConfig.ManagedBy,
	})
}

func seedDeleteResources(t *testing.T, h *FakeHarness, options deleteSeedOptions) {
	t.Helper()

	k8sConfig := h.RuntimeClient().K8sConfig
	labels := map[string]string{
		managedByLabelKey:           options.managedBy,
		dominionEnvironmentLabelKey: options.fullEnvName,
	}

	deploymentName := deleteResourceName(WorkloadKindDeployment, options.fullEnvName, options.app, options.suffix)
	serviceName := deleteResourceName(WorkloadKindService, options.fullEnvName, options.app, options.suffix)
	routeName := deleteResourceName(WorkloadKindHTTPRoute, options.fullEnvName, options.app, options.suffix)

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

func seedOldDeleteResources(t *testing.T, h *FakeHarness, options deleteSeedOptions) {
	t.Helper()

	k8sConfig := h.RuntimeClient().K8sConfig
	labels := map[string]string{
		managedByLabelKey: options.managedBy,
		appLabelKey:       options.app,
		"environment":     legacyEnvironmentName(options.fullEnvName),
	}

	deploymentName := deleteResourceName(WorkloadKindDeployment, options.fullEnvName, options.app, options.suffix)
	serviceName := deleteResourceName(WorkloadKindService, options.fullEnvName, options.app, options.suffix)
	routeName := deleteResourceName(WorkloadKindHTTPRoute, options.fullEnvName, options.app, options.suffix)

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

func deleteResourceName(kind WorkloadKind, fullEnvName, app, suffix string) string {
	name := newObjectName(kind, fullEnvName, app, "gateway")
	if suffix == "" {
		return name
	}

	return name + "-" + suffix
}

func legacyEnvironmentName(fullEnvName string) string {
	if _, env, ok := strings.Cut(strings.TrimSpace(fullEnvName), "/"); ok && env != "" {
		return env
	}

	return strings.TrimSpace(fullEnvName)
}
