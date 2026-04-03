package k8s

import (
	"context"
	"errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiRuntime "k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
)

func TestExecutor_Apply_CreatesResources(t *testing.T) {
	h := NewFakeHarness(t)
	executor := NewExecutor(h.RuntimeClient())
	objects := newExecutorTestDeployObjects()

	if err := executor.Apply(context.Background(), objects); err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}

	dep, err := BuildDeployment(objects.Deployments[0], h.RuntimeClient().K8sConfig)
	if err != nil {
		t.Fatalf("BuildDeployment() failed: %v", err)
	}
	svc, err := BuildService(objects.Services[0], h.RuntimeClient().K8sConfig)
	if err != nil {
		t.Fatalf("BuildService() failed: %v", err)
	}
	route, err := BuildHTTPRoute(objects.HTTPRoutes[0], h.RuntimeClient().K8sConfig)
	if err != nil {
		t.Fatalf("BuildHTTPRoute() failed: %v", err)
	}

	h.AssertDeploymentCreated(dep.Namespace, dep.Name)
	h.AssertServiceCreated(svc.Namespace, svc.Name)
	h.AssertHTTPRouteCreated(route.GetNamespace(), route.GetName())

	h.AssertDeploymentUpdated(dep.Namespace, dep.Name, dep)
	h.AssertServiceUpdated(svc.Namespace, svc.Name, svc)
	h.AssertHTTPRouteUpdated(route.GetNamespace(), route.GetName(), route)
}

func TestExecutor_Apply_UpdatesExistingResources(t *testing.T) {
	h := NewFakeHarness(t)
	executor := NewExecutor(h.RuntimeClient())
	objects := newExecutorTestDeployObjects()
	k8sConfig := h.RuntimeClient().K8sConfig

	dep, err := BuildDeployment(objects.Deployments[0], k8sConfig)
	if err != nil {
		t.Fatalf("BuildDeployment() failed: %v", err)
	}
	svc, err := BuildService(objects.Services[0], k8sConfig)
	if err != nil {
		t.Fatalf("BuildService() failed: %v", err)
	}
	route, err := BuildHTTPRoute(objects.HTTPRoutes[0], k8sConfig)
	if err != nil {
		t.Fatalf("BuildHTTPRoute() failed: %v", err)
	}

	depExisting := dep.DeepCopy()
	depExisting.ResourceVersion = "7"
	depExisting.Spec.Template.Spec.Containers[0].Image = "registry.local/old:tag"
	h.SeedDeployment(depExisting)

	svcExisting := svc.DeepCopy()
	svcExisting.ResourceVersion = "8"
	svcExisting.Spec.Ports[0].Port = 1234
	h.SeedService(svcExisting)

	routeExisting := route.DeepCopy()
	routeExisting.SetResourceVersion("9")
	routeExisting.Object["metadata"].(map[string]any)["labels"].(map[string]any)["seed"] = "old"
	h.SeedHTTPRoute(routeExisting)

	var gotDeploymentRV string
	var gotServiceRV string
	var gotHTTPRouteRV string

	h.typedClient.PrependReactor("update", "*", func(action k8stesting.Action) (bool, apiRuntime.Object, error) {
		updateAction, ok := action.(k8stesting.UpdateAction)
		if !ok {
			return false, nil, nil
		}
		accessor, err := apimeta.Accessor(updateAction.GetObject())
		if err != nil {
			return false, nil, err
		}
		switch accessor.GetName() {
		case dep.Name:
			gotDeploymentRV = accessor.GetResourceVersion()
		case svc.Name:
			gotServiceRV = accessor.GetResourceVersion()
		case route.GetName():
			gotHTTPRouteRV = accessor.GetResourceVersion()
		}
		return false, nil, nil
	})
	h.dynamicClient.PrependReactor("update", "*", func(action k8stesting.Action) (bool, apiRuntime.Object, error) {
		updateAction, ok := action.(k8stesting.UpdateAction)
		if !ok {
			return false, nil, nil
		}
		accessor, err := apimeta.Accessor(updateAction.GetObject())
		if err != nil {
			return false, nil, err
		}
		switch accessor.GetName() {
		case dep.Name:
			gotDeploymentRV = accessor.GetResourceVersion()
		case svc.Name:
			gotServiceRV = accessor.GetResourceVersion()
		case route.GetName():
			gotHTTPRouteRV = accessor.GetResourceVersion()
		}
		return false, nil, nil
	})

	if err := executor.Apply(context.Background(), objects); err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}

	if gotDeploymentRV != "7" {
		t.Fatalf("deployment resourceVersion = %q, want %q", gotDeploymentRV, "7")
	}
	if gotServiceRV != "8" {
		t.Fatalf("service resourceVersion = %q, want %q", gotServiceRV, "8")
	}
	if gotHTTPRouteRV != "9" {
		t.Fatalf("httproute resourceVersion = %q, want %q", gotHTTPRouteRV, "9")
	}

	h.AssertDeploymentUpdated(dep.Namespace, dep.Name, dep)
	h.AssertServiceUpdated(svc.Namespace, svc.Name, svc)
	h.AssertHTTPRouteUpdated(route.GetNamespace(), route.GetName(), route)
}

func TestExecutor_Apply_IsIdempotent(t *testing.T) {
	h := NewFakeHarness(t)
	executor := NewExecutor(h.RuntimeClient())
	objects := newExecutorTestDeployObjects()
	k8sConfig := h.RuntimeClient().K8sConfig

	dep, err := BuildDeployment(objects.Deployments[0], k8sConfig)
	if err != nil {
		t.Fatalf("BuildDeployment() failed: %v", err)
	}
	svc, err := BuildService(objects.Services[0], k8sConfig)
	if err != nil {
		t.Fatalf("BuildService() failed: %v", err)
	}
	route, err := BuildHTTPRoute(objects.HTTPRoutes[0], k8sConfig)
	if err != nil {
		t.Fatalf("BuildHTTPRoute() failed: %v", err)
	}

	if err := executor.Apply(context.Background(), objects); err != nil {
		t.Fatalf("first Apply() failed: %v", err)
	}
	if err := executor.Apply(context.Background(), objects); err != nil {
		t.Fatalf("second Apply() failed: %v", err)
	}

	h.AssertDeploymentCreated(dep.Namespace, dep.Name)
	h.AssertDeploymentUpdated(dep.Namespace, dep.Name, dep)
	h.AssertServiceCreated(svc.Namespace, svc.Name)
	h.AssertServiceUpdated(svc.Namespace, svc.Name, svc)
	h.AssertHTTPRouteCreated(route.GetNamespace(), route.GetName())
	h.AssertHTTPRouteUpdated(route.GetNamespace(), route.GetName(), route)
}

func TestExecutor_Apply_PartialFailure_StopsAndReports(t *testing.T) {
	h := NewFakeHarness(t)
	executor := NewExecutor(h.RuntimeClient())
	objects := newExecutorTestDeployObjects()
	k8sConfig := h.RuntimeClient().K8sConfig

	dep, err := BuildDeployment(objects.Deployments[0], k8sConfig)
	if err != nil {
		t.Fatalf("BuildDeployment() failed: %v", err)
	}
	svc, err := BuildService(objects.Services[0], k8sConfig)
	if err != nil {
		t.Fatalf("BuildService() failed: %v", err)
	}
	route, err := BuildHTTPRoute(objects.HTTPRoutes[0], k8sConfig)
	if err != nil {
		t.Fatalf("BuildHTTPRoute() failed: %v", err)
	}

	wantErr := errors.New("service create boom")
	h.InjectFailure(resourceKindService, operationCreate, wantErr)

	err = executor.Apply(context.Background(), objects)
	if err == nil {
		t.Fatal("Apply() expected error")
	}
	if !strings.Contains(err.Error(), svc.Name) {
		t.Fatalf("error = %v, want contains service name %q", err, svc.Name)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "service") {
		t.Fatalf("error = %v, want contains service kind", err)
	}

	h.AssertDeploymentCreated(dep.Namespace, dep.Name)
	if _, getErr := h.RuntimeClient().DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Get(context.Background(), route.GetName(), metav1.GetOptions{}); getErr == nil {
		t.Fatalf("httproute %s/%s should not exist after failed apply", route.GetNamespace(), route.GetName())
	}
	if _, getErr := h.RuntimeClient().TypedClient.CoreV1().Services(svc.Namespace).Get(context.Background(), svc.Name, metav1.GetOptions{}); getErr == nil {
		t.Fatalf("service %s/%s should not exist after failed apply", svc.Namespace, svc.Name)
	}
}

func newExecutorTestDeployObjects() *DeployObjects {
	return &DeployObjects{
		Deployments: []*DeploymentWorkload{newTestDeploymentWorkload()},
		Services:    []*ServiceWorkload{newTestServiceWorkload()},
		HTTPRoutes:  []*HTTPRouteWorkload{newTestHTTPRouteWorkload()},
	}
}
