package k8s

import (
	"context"
	"errors"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"reflect"
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

	dep, err := BuildDeployment(objects.Deployments[0])
	if err != nil {
		t.Fatalf("BuildDeployment() failed: %v", err)
	}
	svc, err := BuildService(objects.Deployments[0])
	if err != nil {
		t.Fatalf("BuildService() failed: %v", err)
	}
	route, err := BuildHTTPRoute(objects.HTTPRoutes[0])
	if err != nil {
		t.Fatalf("BuildHTTPRoute() failed: %v", err)
	}

	h.AssertDeploymentCreated(dep.Namespace, dep.Name)
	h.AssertServiceCreated(svc.Namespace, svc.Name)
	h.AssertHTTPRouteCreated(route.GetNamespace(), route.GetName())
	assertStoredDeploymentReservedEnv(t, h, dep.Namespace, dep.Name)

	h.AssertDeploymentUpdated(dep.Namespace, dep.Name, dep)
	h.AssertServiceUpdated(svc.Namespace, svc.Name, svc)
	h.AssertHTTPRouteUpdated(route.GetNamespace(), route.GetName(), route)
}

func TestExecutor_Apply_UpdatesExistingResources(t *testing.T) {
	h := NewFakeHarness(t)
	executor := NewExecutor(h.RuntimeClient())
	objects := newExecutorTestDeployObjects()
	dep, err := BuildDeployment(objects.Deployments[0])
	if err != nil {
		t.Fatalf("BuildDeployment() failed: %v", err)
	}
	svc, err := BuildService(objects.Deployments[0])
	if err != nil {
		t.Fatalf("BuildService() failed: %v", err)
	}
	route, err := BuildHTTPRoute(objects.HTTPRoutes[0])
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
	dep, err := BuildDeployment(objects.Deployments[0])
	if err != nil {
		t.Fatalf("BuildDeployment() failed: %v", err)
	}
	svc, err := BuildService(objects.Deployments[0])
	if err != nil {
		t.Fatalf("BuildService() failed: %v", err)
	}
	route, err := BuildHTTPRoute(objects.HTTPRoutes[0])
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
	dep, err := BuildDeployment(objects.Deployments[0])
	if err != nil {
		t.Fatalf("BuildDeployment() failed: %v", err)
	}
	svc, err := BuildService(objects.Deployments[0])
	if err != nil {
		t.Fatalf("BuildService() failed: %v", err)
	}
	route, err := BuildHTTPRoute(objects.HTTPRoutes[0])
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

func TestExecutor_Apply_MongoDB_CreatesResourcesInOrder(t *testing.T) {
	// given
	stubLoadK8sConfig(t, newTestK8sConfigWithMongoProfile())
	h := NewFakeHarness(t)
	executor := NewExecutor(h.RuntimeClient())
	objects := newExecutorTestMongoDeployObjects()
	workload := objects.MongoDBWorkloads[0]

	var createOrder []string
	h.typedClient.PrependReactor("create", "*", func(action k8stesting.Action) (bool, apiRuntime.Object, error) {
		switch action.GetResource().Resource {
		case "persistentvolumeclaims":
			createOrder = append(createOrder, resourceKindPVC)
		default:
			resourceType, ok := normalizeResourceTypeFromAction(action.GetResource().Resource)
			if ok {
				createOrder = append(createOrder, resourceType)
			}
		}
		return false, nil, nil
	})

	// when
	if err := executor.Apply(context.Background(), objects); err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}

	// then
	wantOrder := []string{resourceKindPVC, resourceKindSecret, resourceKindDeployment, resourceKindService}
	if !reflect.DeepEqual(createOrder, wantOrder) {
		t.Fatalf("create order = %v, want %v", createOrder, wantOrder)
	}

	h.AssertPVCCreated("team-dev", workload.PVCResourceName())
	h.AssertSecretCreated("team-dev", workload.SecretResourceName())
	h.AssertDeploymentCreated("team-dev", workload.ResourceName())
	h.AssertServiceCreated("team-dev", workload.ServiceResourceName())
	assertStoredMongoDeploymentReservedEnv(t, h, "team-dev", workload.ResourceName())
}

func TestExecutor_Apply_MongoDB_IncompatiblePVCStopsAndReports(t *testing.T) {
	// given
	stubLoadK8sConfig(t, newTestK8sConfigWithMongoProfile())
	h := NewFakeHarness(t)
	executor := NewExecutor(h.RuntimeClient())
	objects := newExecutorTestMongoDeployObjects()
	workload := objects.MongoDBWorkloads[0]

	existingPVC := newMongoPVCWithMutation(func(pvc *corev1.PersistentVolumeClaim) {
		pvc.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("2Gi")
	})
	h.SeedPVC(existingPVC)

	// when
	err := executor.Apply(context.Background(), objects)

	// then
	if err == nil {
		t.Fatal("Apply() expected error")
	}
	if !strings.Contains(err.Error(), workload.PVCResourceName()) {
		t.Fatalf("error = %v, want contains pvc name %q", err, workload.PVCResourceName())
	}
	if !strings.Contains(strings.ToLower(err.Error()), resourceKindPVC) {
		t.Fatalf("error = %v, want contains pvc kind", err)
	}

	h.AssertPVCCreated("team-dev", workload.PVCResourceName())
	h.AssertSecretDeleted("team-dev", workload.SecretResourceName())
	h.AssertDeploymentDeleted("team-dev", workload.ResourceName())
	h.AssertServiceDeleted("team-dev", workload.ServiceResourceName())
}

func newExecutorTestDeployObjects() *DeployObjects {
	return &DeployObjects{
		Deployments: []*DeploymentWorkload{newTestDeploymentWorkload()},
		HTTPRoutes:  []*HTTPRouteWorkload{newTestHTTPRouteWorkload()},
	}
}

func newExecutorTestMongoDeployObjects() *DeployObjects {
	workload := newTestMongoDBWorkload()
	workload.Persistence.Enabled = true

	return &DeployObjects{
		MongoDBWorkloads: []*MongoDBWorkload{workload},
	}
}

func assertStoredDeploymentReservedEnv(t *testing.T, h *FakeHarness, namespace string, name string) {
	t.Helper()

	deployment, err := h.getDeployment(namespace, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("containers len = %d, want 1", len(deployment.Spec.Template.Spec.Containers))
	}
	assertExecutorReservedEnv(t, deployment.Spec.Template.Spec.Containers[0].Env)
}

func assertStoredMongoDeploymentReservedEnv(t *testing.T, h *FakeHarness, namespace string, name string) {
	t.Helper()

	deployment, err := h.getDeployment(namespace, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("containers len = %d, want 1", len(deployment.Spec.Template.Spec.Containers))
	}
	assertMongoReservedEnv(t, deployment.Spec.Template.Spec.Containers[0].Env)
	if len(deployment.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("init containers len = %d, want 1", len(deployment.Spec.Template.Spec.InitContainers))
	}
	assertMongoReservedEnv(t, deployment.Spec.Template.Spec.InitContainers[0].Env)
}

func assertMongoReservedEnv(t *testing.T, env []corev1.EnvVar) {
	t.Helper()

	if len(env) < 3 {
		t.Fatalf("env len = %d, want >= 3", len(env))
	}
	if env[0].Name != reservedEnvNameDominionApp || env[0].Value != "grpc-hello-world" {
		t.Fatalf("env[0] = %#v, want DOMINION_APP literal", env[0])
	}
	if env[1].Name != reservedEnvNameDominionEnvironment || env[1].Value != "dev" {
		t.Fatalf("env[1] = %#v, want DOMINION_ENVIRONMENT literal", env[1])
	}
	if env[2].Name != reservedEnvNamePodNamespace || env[2].ValueFrom == nil || env[2].ValueFrom.FieldRef == nil || env[2].ValueFrom.FieldRef.FieldPath != mongoPodFieldPathNS {
		t.Fatalf("env[2] = %#v, want POD_NAMESPACE fieldRef %q", env[2], mongoPodFieldPathNS)
	}
}

func assertExecutorReservedEnv(t *testing.T, env []corev1.EnvVar) {
	t.Helper()

	if len(env) != 3 {
		t.Fatalf("env len = %d, want 3", len(env))
	}
	if env[0].Name != reservedEnvNameDominionApp || env[0].Value != "grpc-hello-world" {
		t.Fatalf("env[0] = %#v, want DOMINION_APP literal", env[0])
	}
	if env[1].Name != reservedEnvNameDominionEnvironment || env[1].Value != "dev" {
		t.Fatalf("env[1] = %#v, want DOMINION_ENVIRONMENT literal", env[1])
	}
	if env[2].Name != reservedEnvNamePodNamespace || env[2].Value != "default" {
		t.Fatalf("env[2] = %#v, want POD_NAMESPACE literal", env[2])
	}
}
