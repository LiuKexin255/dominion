package k8s

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	resourceKindDeployment = "deployment"
	resourceKindService    = "service"
	resourceKindHTTPRoute  = "httproute"

	operationCreate = "create"
	operationUpdate = "update"
	operationDelete = "delete"
)

type operationRecord struct {
	resourceType string
	operation    string
	namespace    string
	name         string
}

type failureKey struct {
	resourceType string
	operation    string
}

type FakeHarness struct {
	t *testing.T

	typedClient   *kubernetesfake.Clientset
	dynamicClient *dynamicfake.FakeDynamicClient

	mu         sync.Mutex
	failures   map[failureKey]error
	operations []operationRecord
}

func NewFakeHarness(t *testing.T) *FakeHarness {
	t.Helper()

	h := &FakeHarness{
		t:             t,
		typedClient:   kubernetesfake.NewSimpleClientset(),
		dynamicClient: newFakeDynamicClient(),
		failures:      make(map[failureKey]error),
		operations:    nil,
	}

	h.typedClient.PrependReactor("*", "*", h.reactor)
	h.dynamicClient.PrependReactor("*", "*", h.reactor)

	return h
}

func (h *FakeHarness) RuntimeClient() *RuntimeClient {
	return &RuntimeClient{
		TypedClient:   h.typedClient,
		DynamicClient: h.dynamicClient,
		K8sConfig:     LoadK8sConfig(),
	}
}

func (h *FakeHarness) SeedDeployment(d *appsv1.Deployment) {
	h.t.Helper()
	if d == nil {
		h.t.Fatal("SeedDeployment() requires a deployment")
	}
	if err := h.typedClient.Tracker().Add(d.DeepCopy()); err != nil {
		h.t.Fatalf("seed deployment %s/%s failed: %v", d.Namespace, d.Name, err)
	}
}

func (h *FakeHarness) SeedService(s *corev1.Service) {
	h.t.Helper()
	if s == nil {
		h.t.Fatal("SeedService() requires a service")
	}
	if err := h.typedClient.Tracker().Add(s.DeepCopy()); err != nil {
		h.t.Fatalf("seed service %s/%s failed: %v", s.Namespace, s.Name, err)
	}
}

func (h *FakeHarness) SeedHTTPRoute(r *unstructured.Unstructured) {
	h.t.Helper()
	if r == nil {
		h.t.Fatal("SeedHTTPRoute() requires an HTTPRoute")
	}
	if err := h.dynamicClient.Tracker().Add(r.DeepCopy()); err != nil {
		h.t.Fatalf("seed httproute %s/%s failed: %v", r.GetNamespace(), r.GetName(), err)
	}
}

func (h *FakeHarness) InjectFailure(resourceType, operation string, err error) {
	h.t.Helper()
	if err == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures[failureKey{resourceType: normalizeResourceType(resourceType), operation: normalizeOperation(operation)}] = err
}

func (h *FakeHarness) AssertDeploymentCreated(namespace, name string) {
	h.assertOperation(resourceKindDeployment, operationCreate, namespace, name, true)
}

func (h *FakeHarness) AssertDeploymentUpdated(namespace, name string) {
	h.assertOperation(resourceKindDeployment, operationUpdate, namespace, name, true)
}

func (h *FakeHarness) AssertDeploymentDeleted(namespace, name string) {
	h.assertOperation(resourceKindDeployment, operationDelete, namespace, name, false)
}

func (h *FakeHarness) AssertServiceCreated(namespace, name string) {
	h.assertOperation(resourceKindService, operationCreate, namespace, name, true)
}

func (h *FakeHarness) AssertServiceUpdated(namespace, name string) {
	h.assertOperation(resourceKindService, operationUpdate, namespace, name, true)
}

func (h *FakeHarness) AssertServiceDeleted(namespace, name string) {
	h.assertOperation(resourceKindService, operationDelete, namespace, name, false)
}

func (h *FakeHarness) AssertHTTPRouteCreated(namespace, name string) {
	h.assertOperation(resourceKindHTTPRoute, operationCreate, namespace, name, true)
}

func (h *FakeHarness) AssertHTTPRouteUpdated(namespace, name string) {
	h.assertOperation(resourceKindHTTPRoute, operationUpdate, namespace, name, true)
}

func (h *FakeHarness) AssertHTTPRouteDeleted(namespace, name string) {
	h.assertOperation(resourceKindHTTPRoute, operationDelete, namespace, name, false)
}

func (h *FakeHarness) reactor(action k8stesting.Action) (bool, apiRuntime.Object, error) {
	resourceType, ok := normalizeResourceTypeFromAction(action.GetResource().Resource)
	if !ok {
		return false, nil, nil
	}

	operation, ok := normalizeOperationFromVerb(action.GetVerb())
	if !ok {
		return false, nil, nil
	}

	if err := h.failureFor(resourceType, operation); err != nil {
		return true, nil, err
	}

	name, namespace := actionIdentity(action)
	h.mu.Lock()
	h.operations = append(h.operations, operationRecord{
		resourceType: resourceType,
		operation:    operation,
		namespace:    namespace,
		name:         name,
	})
	h.mu.Unlock()

	return false, nil, nil
}

func (h *FakeHarness) failureFor(resourceType, operation string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.failures[failureKey{resourceType: resourceType, operation: operation}]
}

func (h *FakeHarness) assertOperation(resourceType, operation, namespace, name string, wantPresent bool) {
	h.t.Helper()

	if !h.hasOperation(resourceType, operation, namespace, name) {
		h.t.Fatalf("%s %s %s/%s was not recorded; operations=%s", resourceType, operation, namespace, name, h.describeOperations())
	}

	if wantPresent {
		if err := h.assertExists(resourceType, namespace, name); err != nil {
			h.t.Fatal(err)
		}
		return
	}

	if err := h.assertMissing(resourceType, namespace, name); err != nil {
		h.t.Fatal(err)
	}
}

func (h *FakeHarness) hasOperation(resourceType, operation, namespace, name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, op := range h.operations {
		if op.resourceType == resourceType && op.operation == operation && op.namespace == namespace && op.name == name {
			return true
		}
	}
	return false
}

func (h *FakeHarness) describeOperations() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.operations) == 0 {
		return "[]"
	}

	parts := make([]string, 0, len(h.operations))
	for _, op := range h.operations {
		parts = append(parts, fmt.Sprintf("%s %s %s/%s", op.resourceType, op.operation, op.namespace, op.name))
	}

	return "[" + strings.Join(parts, ", ") + "]"
}

func (h *FakeHarness) assertExists(resourceType, namespace, name string) error {
	switch resourceType {
	case resourceKindDeployment:
		_, err := h.typedClient.AppsV1().Deployments(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("deployment %s/%s should exist: %w", namespace, name, err)
		}
	case resourceKindService:
		_, err := h.typedClient.CoreV1().Services(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("service %s/%s should exist: %w", namespace, name, err)
		}
	case resourceKindHTTPRoute:
		_, err := h.dynamicClient.Resource(httpRouteGVR()).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("httproute %s/%s should exist: %w", namespace, name, err)
		}
	default:
		return fmt.Errorf("unsupported resource type %q", resourceType)
	}

	return nil
}

func (h *FakeHarness) assertMissing(resourceType, namespace, name string) error {
	switch resourceType {
	case resourceKindDeployment:
		_, err := h.typedClient.AppsV1().Deployments(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err == nil {
			return fmt.Errorf("deployment %s/%s should have been deleted", namespace, name)
		}
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("deployment %s/%s delete check failed: %w", namespace, name, err)
		}
	case resourceKindService:
		_, err := h.typedClient.CoreV1().Services(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err == nil {
			return fmt.Errorf("service %s/%s should have been deleted", namespace, name)
		}
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("service %s/%s delete check failed: %w", namespace, name, err)
		}
	case resourceKindHTTPRoute:
		_, err := h.dynamicClient.Resource(httpRouteGVR()).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err == nil {
			return fmt.Errorf("httproute %s/%s should have been deleted", namespace, name)
		}
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("httproute %s/%s delete check failed: %w", namespace, name, err)
		}
	default:
		return fmt.Errorf("unsupported resource type %q", resourceType)
	}

	return nil
}

func actionIdentity(action k8stesting.Action) (string, string) {
	resource := strings.ToLower(action.GetResource().Resource)
	if a, ok := action.(k8stesting.CreateAction); ok {
		return objectIdentity(a.GetObject())
	}
	if a, ok := action.(k8stesting.UpdateAction); ok {
		return objectIdentity(a.GetObject())
	}
	if a, ok := action.(k8stesting.DeleteAction); ok {
		return a.GetName(), a.GetNamespace()
	}
	return "", action.GetNamespace() + ":" + resource
}

func objectIdentity(obj apiRuntime.Object) (string, string) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return "", ""
	}
	return accessor.GetName(), accessor.GetNamespace()
}

func normalizeResourceType(value string) string {
	resourceType, _ := normalizeResourceTypeFromAction(value)
	return resourceType
}

func normalizeResourceTypeFromAction(resource string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(resource)) {
	case resourceKindDeployment, "deployments":
		return resourceKindDeployment, true
	case resourceKindService, "services":
		return resourceKindService, true
	case resourceKindHTTPRoute, "httproutes":
		return resourceKindHTTPRoute, true
	default:
		return "", false
	}
}

func normalizeOperation(operation string) string {
	op, _ := normalizeOperationFromVerb(operation)
	return op
}

func normalizeOperationFromVerb(verb string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(verb)) {
	case operationCreate:
		return operationCreate, true
	case operationUpdate:
		return operationUpdate, true
	case operationDelete:
		return operationDelete, true
	default:
		return "", false
	}
}

func newFakeDynamicClient() *dynamicfake.FakeDynamicClient {
	scheme := apiRuntime.NewScheme()
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = gatewayv1.AddToScheme(scheme)

	return dynamicfake.NewSimpleDynamicClient(scheme)
}

func httpRouteGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "httproutes",
	}
}
