package k8s

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestFakeHarness_RuntimeClientAndSeeding(t *testing.T) {
	h := NewFakeHarness(t)
	client := h.RuntimeClient()
	if client == nil {
		t.Fatal("RuntimeClient() returned nil")
	}

	dep := testFakeDeployment("team-a", "gateway")
	svc := testFakeService("team-a", "gateway")
	route := testFakeHTTPRoute("team-a", "gateway")

	h.SeedDeployment(dep)
	h.SeedService(svc)
	h.SeedHTTPRoute(route)

	gotDep, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Get(context.Background(), dep.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment failed: %v", err)
	}
	if gotDep.Name != dep.Name || gotDep.Namespace != dep.Namespace {
		t.Fatalf("deployment = %s/%s, want %s/%s", gotDep.Namespace, gotDep.Name, dep.Namespace, dep.Name)
	}

	gotSvc, err := client.TypedClient.CoreV1().Services(svc.Namespace).Get(context.Background(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get service failed: %v", err)
	}
	if gotSvc.Name != svc.Name || gotSvc.Namespace != svc.Namespace {
		t.Fatalf("service = %s/%s, want %s/%s", gotSvc.Namespace, gotSvc.Name, svc.Namespace, svc.Name)
	}

	gotRoute, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Get(context.Background(), route.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get httproute failed: %v", err)
	}
	if gotRoute.GetName() != route.GetName() || gotRoute.GetNamespace() != route.GetNamespace() {
		t.Fatalf("httproute = %s/%s, want %s/%s", gotRoute.GetNamespace(), gotRoute.GetName(), route.GetNamespace(), route.GetName())
	}
}

func TestFakeHarness_DeploymentCreated(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		appName   string
	}{
		{name: "default namespace", namespace: "default", appName: "app1"},
		{name: "custom namespace", namespace: "team-a", appName: "gateway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewFakeHarness(t)
			client := h.RuntimeClient()
			ctx := context.Background()
			dep := testFakeDeployment(tt.namespace, tt.appName)

			if _, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
				t.Fatalf("create deployment failed: %v", err)
			}
			assertRecordedOperations(t, h, []operationRecord{{resourceType: resourceKindDeployment, operation: operationCreate, namespace: dep.Namespace, name: dep.Name}})
			h.AssertDeploymentCreated(dep.Namespace, dep.Name)
		})
	}
}

func TestFakeHarness_DeploymentUpdated(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		appName   string
	}{
		{name: "default namespace", namespace: "default", appName: "app1"},
		{name: "custom namespace", namespace: "team-a", appName: "gateway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewFakeHarness(t)
			client := h.RuntimeClient()
			ctx := context.Background()
			dep := testFakeDeployment(tt.namespace, tt.appName)

			if _, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
				t.Fatalf("create deployment failed: %v", err)
			}
			dep.Labels["version"] = "v2"
			if _, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
				t.Fatalf("update deployment failed: %v", err)
			}
			assertRecordedOperations(t, h, []operationRecord{
				{resourceType: resourceKindDeployment, operation: operationCreate, namespace: dep.Namespace, name: dep.Name},
				{resourceType: resourceKindDeployment, operation: operationUpdate, namespace: dep.Namespace, name: dep.Name},
			})
			h.AssertDeploymentCreated(dep.Namespace, dep.Name)
			h.AssertDeploymentUpdated(dep.Namespace, dep.Name)
		})
	}
}

func TestFakeHarness_DeploymentDeleted(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		appName   string
	}{
		{name: "default namespace", namespace: "default", appName: "app1"},
		{name: "custom namespace", namespace: "team-a", appName: "gateway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewFakeHarness(t)
			client := h.RuntimeClient()
			ctx := context.Background()
			dep := testFakeDeployment(tt.namespace, tt.appName)

			if _, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
				t.Fatalf("create deployment failed: %v", err)
			}
			if err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Delete(ctx, dep.Name, metav1.DeleteOptions{}); err != nil {
				t.Fatalf("delete deployment failed: %v", err)
			}
			assertRecordedOperations(t, h, []operationRecord{
				{resourceType: resourceKindDeployment, operation: operationCreate, namespace: dep.Namespace, name: dep.Name},
				{resourceType: resourceKindDeployment, operation: operationDelete, namespace: dep.Namespace, name: dep.Name},
			})
			h.AssertDeploymentDeleted(dep.Namespace, dep.Name)
		})
	}
}

func TestFakeHarness_ServiceCreated(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		appName   string
	}{
		{name: "default namespace", namespace: "default", appName: "app1"},
		{name: "custom namespace", namespace: "team-a", appName: "gateway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewFakeHarness(t)
			client := h.RuntimeClient()
			ctx := context.Background()
			svc := testFakeService(tt.namespace, tt.appName)

			if _, err := client.TypedClient.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
				t.Fatalf("create service failed: %v", err)
			}
			assertRecordedOperations(t, h, []operationRecord{{resourceType: resourceKindService, operation: operationCreate, namespace: svc.Namespace, name: svc.Name}})
			h.AssertServiceCreated(svc.Namespace, svc.Name)
		})
	}
}

func TestFakeHarness_ServiceUpdated(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		appName   string
	}{
		{name: "default namespace", namespace: "default", appName: "app1"},
		{name: "custom namespace", namespace: "team-a", appName: "gateway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewFakeHarness(t)
			client := h.RuntimeClient()
			ctx := context.Background()
			svc := testFakeService(tt.namespace, tt.appName)

			if _, err := client.TypedClient.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
				t.Fatalf("create service failed: %v", err)
			}
			svc.Labels["version"] = "v2"
			if _, err := client.TypedClient.CoreV1().Services(svc.Namespace).Update(ctx, svc, metav1.UpdateOptions{}); err != nil {
				t.Fatalf("update service failed: %v", err)
			}
			assertRecordedOperations(t, h, []operationRecord{
				{resourceType: resourceKindService, operation: operationCreate, namespace: svc.Namespace, name: svc.Name},
				{resourceType: resourceKindService, operation: operationUpdate, namespace: svc.Namespace, name: svc.Name},
			})
			h.AssertServiceCreated(svc.Namespace, svc.Name)
			h.AssertServiceUpdated(svc.Namespace, svc.Name)
		})
	}
}

func TestFakeHarness_ServiceDeleted(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		appName   string
	}{
		{name: "default namespace", namespace: "default", appName: "app1"},
		{name: "custom namespace", namespace: "team-a", appName: "gateway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewFakeHarness(t)
			client := h.RuntimeClient()
			ctx := context.Background()
			svc := testFakeService(tt.namespace, tt.appName)

			if _, err := client.TypedClient.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
				t.Fatalf("create service failed: %v", err)
			}
			if err := client.TypedClient.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{}); err != nil {
				t.Fatalf("delete service failed: %v", err)
			}
			assertRecordedOperations(t, h, []operationRecord{
				{resourceType: resourceKindService, operation: operationCreate, namespace: svc.Namespace, name: svc.Name},
				{resourceType: resourceKindService, operation: operationDelete, namespace: svc.Namespace, name: svc.Name},
			})
			h.AssertServiceDeleted(svc.Namespace, svc.Name)
		})
	}
}

func TestFakeHarness_HTTPRouteCreated(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		appName   string
	}{
		{name: "default namespace", namespace: "default", appName: "app1"},
		{name: "custom namespace", namespace: "team-a", appName: "gateway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewFakeHarness(t)
			client := h.RuntimeClient()
			ctx := context.Background()
			route := testFakeHTTPRoute(tt.namespace, tt.appName)

			if _, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Create(ctx, route, metav1.CreateOptions{}); err != nil {
				t.Fatalf("create httproute failed: %v", err)
			}
			assertRecordedOperations(t, h, []operationRecord{{resourceType: resourceKindHTTPRoute, operation: operationCreate, namespace: route.GetNamespace(), name: route.GetName()}})
			h.AssertHTTPRouteCreated(route.GetNamespace(), route.GetName())
		})
	}
}

func TestFakeHarness_HTTPRouteUpdated(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		appName   string
	}{
		{name: "default namespace", namespace: "default", appName: "app1"},
		{name: "custom namespace", namespace: "team-a", appName: "gateway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewFakeHarness(t)
			client := h.RuntimeClient()
			ctx := context.Background()
			route := testFakeHTTPRoute(tt.namespace, tt.appName)

			if _, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Create(ctx, route, metav1.CreateOptions{}); err != nil {
				t.Fatalf("create httproute failed: %v", err)
			}
			route.Object["metadata"].(map[string]any)["labels"].(map[string]any)["version"] = "v2"
			if _, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Update(ctx, route, metav1.UpdateOptions{}); err != nil {
				t.Fatalf("update httproute failed: %v", err)
			}
			assertRecordedOperations(t, h, []operationRecord{
				{resourceType: resourceKindHTTPRoute, operation: operationCreate, namespace: route.GetNamespace(), name: route.GetName()},
				{resourceType: resourceKindHTTPRoute, operation: operationUpdate, namespace: route.GetNamespace(), name: route.GetName()},
			})
			h.AssertHTTPRouteCreated(route.GetNamespace(), route.GetName())
			h.AssertHTTPRouteUpdated(route.GetNamespace(), route.GetName())
		})
	}
}

func TestFakeHarness_HTTPRouteDeleted(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		appName   string
	}{
		{name: "default namespace", namespace: "default", appName: "app1"},
		{name: "custom namespace", namespace: "team-a", appName: "gateway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewFakeHarness(t)
			client := h.RuntimeClient()
			ctx := context.Background()
			route := testFakeHTTPRoute(tt.namespace, tt.appName)

			if _, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Create(ctx, route, metav1.CreateOptions{}); err != nil {
				t.Fatalf("create httproute failed: %v", err)
			}
			if err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Delete(ctx, route.GetName(), metav1.DeleteOptions{}); err != nil {
				t.Fatalf("delete httproute failed: %v", err)
			}
			assertRecordedOperations(t, h, []operationRecord{
				{resourceType: resourceKindHTTPRoute, operation: operationCreate, namespace: route.GetNamespace(), name: route.GetName()},
				{resourceType: resourceKindHTTPRoute, operation: operationDelete, namespace: route.GetNamespace(), name: route.GetName()},
			})
			h.AssertHTTPRouteDeleted(route.GetNamespace(), route.GetName())
		})
	}
}

func TestFakeHarness_InjectFailure_DeploymentCreate(t *testing.T) {
	h := NewFakeHarness(t)
	wantErr := errors.New("boom")
	h.InjectFailure(resourceKindDeployment, operationCreate, wantErr)

	dep := testFakeDeployment("team-c", "gateway")
	_, err := h.RuntimeClient().TypedClient.AppsV1().Deployments(dep.Namespace).Create(context.Background(), dep, metav1.CreateOptions{})

	if err == nil {
		t.Fatal("expected injected failure")
	}
	if !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("error = %v, want contains %q", err, wantErr.Error())
	}
}

func TestFakeHarness_InjectFailure_ServiceUpdate(t *testing.T) {
	h := NewFakeHarness(t)
	wantErr := errors.New("boom")
	h.InjectFailure(resourceKindService, operationUpdate, wantErr)

	svc := testFakeService("team-c", "gateway")
	h.SeedService(svc)
	svc.Labels["version"] = "v2"
	_, err := h.RuntimeClient().TypedClient.CoreV1().Services(svc.Namespace).Update(context.Background(), svc, metav1.UpdateOptions{})

	if err == nil {
		t.Fatal("expected injected failure")
	}
	if !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("error = %v, want contains %q", err, wantErr.Error())
	}
}

func TestFakeHarness_InjectFailure_HTTPRouteDelete(t *testing.T) {
	h := NewFakeHarness(t)
	wantErr := errors.New("boom")
	h.InjectFailure(resourceKindHTTPRoute, operationDelete, wantErr)

	route := testFakeHTTPRoute("team-c", "gateway")
	h.SeedHTTPRoute(route)
	err := h.RuntimeClient().DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Delete(context.Background(), route.GetName(), metav1.DeleteOptions{})

	if err == nil {
		t.Fatal("expected injected failure")
	}
	if !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("error = %v, want contains %q", err, wantErr.Error())
	}
}

func TestFakeHarness_CreateSequence(t *testing.T) {
	h := NewFakeHarness(t)
	client := h.RuntimeClient()
	ctx := context.Background()

	dep := testFakeDeployment("team-d", "gateway")
	svc := testFakeService("team-d", "gateway")
	route := testFakeHTTPRoute("team-d", "gateway")

	if _, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment failed: %v", err)
	}
	if _, err := client.TypedClient.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create service failed: %v", err)
	}
	if _, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Create(ctx, route, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create httproute failed: %v", err)
	}

	assertRecordedOperations(t, h, []operationRecord{
		{resourceType: resourceKindDeployment, operation: operationCreate, namespace: dep.Namespace, name: dep.Name},
		{resourceType: resourceKindService, operation: operationCreate, namespace: svc.Namespace, name: svc.Name},
		{resourceType: resourceKindHTTPRoute, operation: operationCreate, namespace: route.GetNamespace(), name: route.GetName()},
	})
}

func TestFakeHarness_UpdateSequence(t *testing.T) {
	h := NewFakeHarness(t)
	client := h.RuntimeClient()
	ctx := context.Background()

	dep := testFakeDeployment("team-d", "gateway")
	svc := testFakeService("team-d", "gateway")
	route := testFakeHTTPRoute("team-d", "gateway")

	if _, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment failed: %v", err)
	}
	dep.Labels["sequence"] = "1"
	if _, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update deployment failed: %v", err)
	}

	if _, err := client.TypedClient.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create service failed: %v", err)
	}
	svc.Labels["sequence"] = "1"
	if _, err := client.TypedClient.CoreV1().Services(svc.Namespace).Update(ctx, svc, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update service failed: %v", err)
	}

	if _, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Create(ctx, route, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create httproute failed: %v", err)
	}
	route.Object["metadata"].(map[string]any)["labels"].(map[string]any)["sequence"] = "1"
	if _, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Update(ctx, route, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update httproute failed: %v", err)
	}

	assertRecordedOperations(t, h, []operationRecord{
		{resourceType: resourceKindDeployment, operation: operationCreate, namespace: dep.Namespace, name: dep.Name},
		{resourceType: resourceKindDeployment, operation: operationUpdate, namespace: dep.Namespace, name: dep.Name},
		{resourceType: resourceKindService, operation: operationCreate, namespace: svc.Namespace, name: svc.Name},
		{resourceType: resourceKindService, operation: operationUpdate, namespace: svc.Namespace, name: svc.Name},
		{resourceType: resourceKindHTTPRoute, operation: operationCreate, namespace: route.GetNamespace(), name: route.GetName()},
		{resourceType: resourceKindHTTPRoute, operation: operationUpdate, namespace: route.GetNamespace(), name: route.GetName()},
	})
}

func TestFakeHarness_DeleteSequence(t *testing.T) {
	h := NewFakeHarness(t)
	client := h.RuntimeClient()
	ctx := context.Background()

	dep := testFakeDeployment("team-d", "gateway")
	svc := testFakeService("team-d", "gateway")
	route := testFakeHTTPRoute("team-d", "gateway")

	if _, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create deployment failed: %v", err)
	}
	if err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Delete(ctx, dep.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete deployment failed: %v", err)
	}

	if _, err := client.TypedClient.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create service failed: %v", err)
	}
	if err := client.TypedClient.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete service failed: %v", err)
	}

	if _, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Create(ctx, route, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create httproute failed: %v", err)
	}
	if err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Delete(ctx, route.GetName(), metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete httproute failed: %v", err)
	}

	assertRecordedOperations(t, h, []operationRecord{
		{resourceType: resourceKindDeployment, operation: operationCreate, namespace: dep.Namespace, name: dep.Name},
		{resourceType: resourceKindDeployment, operation: operationDelete, namespace: dep.Namespace, name: dep.Name},
		{resourceType: resourceKindService, operation: operationCreate, namespace: svc.Namespace, name: svc.Name},
		{resourceType: resourceKindService, operation: operationDelete, namespace: svc.Namespace, name: svc.Name},
		{resourceType: resourceKindHTTPRoute, operation: operationCreate, namespace: route.GetNamespace(), name: route.GetName()},
		{resourceType: resourceKindHTTPRoute, operation: operationDelete, namespace: route.GetNamespace(), name: route.GetName()},
	})
}

func testFakeDeployment(namespace, name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				"app": name,
			},
		},
	}
}

func testFakeService(namespace, name string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				"app": name,
			},
		},
	}
}

func testFakeHTTPRoute(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata": map[string]any{
			"namespace": namespace,
			"name":      name,
			"labels": map[string]any{
				"app": name,
			},
		},
	}}
}

func assertRecordedOperations(t *testing.T, h *FakeHarness, want []operationRecord) {
	t.Helper()

	h.mu.Lock()
	got := append([]operationRecord(nil), h.operations...)
	h.mu.Unlock()

	if len(got) != len(want) {
		t.Fatalf("operations len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("operation[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
