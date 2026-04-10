package k8s

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	resourceKindPVC        = "pvc"
	resourceKindSecret     = "secret"

	operationCreate = "create"
	operationUpdate = "update"
	operationDelete = "delete"
)

type failureKey struct {
	resourceType string
	operation    string
}

type FakeHarness struct {
	t *testing.T

	typedClient   *kubernetesfake.Clientset
	dynamicClient *dynamicfake.FakeDynamicClient

	mu       sync.Mutex
	failures map[failureKey]error
}

func NewFakeHarness(t *testing.T) *FakeHarness {
	t.Helper()

	h := &FakeHarness{
		t:             t,
		typedClient:   kubernetesfake.NewSimpleClientset(),
		dynamicClient: newFakeDynamicClient(),
		failures:      make(map[failureKey]error),
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

// SeedPVC seeds a PersistentVolumeClaim into the fake typed client.
func (h *FakeHarness) SeedPVC(pvc *corev1.PersistentVolumeClaim) {
	h.t.Helper()
	if pvc == nil {
		h.t.Fatal("SeedPVC() requires a pvc")
	}
	if err := h.typedClient.Tracker().Add(pvc.DeepCopy()); err != nil {
		h.t.Fatalf("seed pvc %s/%s failed: %v", pvc.Namespace, pvc.Name, err)
	}
}

// SeedSecret seeds a Secret into the fake typed client.
func (h *FakeHarness) SeedSecret(secret *corev1.Secret) {
	h.t.Helper()
	if secret == nil {
		h.t.Fatal("SeedSecret() requires a secret")
	}
	if err := h.typedClient.Tracker().Add(secret.DeepCopy()); err != nil {
		h.t.Fatalf("seed secret %s/%s failed: %v", secret.Namespace, secret.Name, err)
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
	h.t.Helper()
	if _, err := h.getDeployment(namespace, name); err != nil {
		h.t.Fatal(err)
	}
}

func (h *FakeHarness) AssertDeploymentUpdated(namespace, name string, expected *appsv1.Deployment) {
	h.t.Helper()
	stored, err := h.getDeployment(namespace, name)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := assertDeploymentMatches(stored, expected); err != nil {
		h.t.Fatal(err)
	}
}

func (h *FakeHarness) AssertDeploymentDeleted(namespace, name string) {
	h.t.Helper()
	if err := h.assertDeploymentMissing(namespace, name); err != nil {
		h.t.Fatal(err)
	}
}

func (h *FakeHarness) AssertServiceCreated(namespace, name string) {
	h.t.Helper()
	if _, err := h.getService(namespace, name); err != nil {
		h.t.Fatal(err)
	}
}

func (h *FakeHarness) AssertServiceUpdated(namespace, name string, expected *corev1.Service) {
	h.t.Helper()
	stored, err := h.getService(namespace, name)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := assertServiceMatches(stored, expected); err != nil {
		h.t.Fatal(err)
	}
}

func (h *FakeHarness) AssertServiceDeleted(namespace, name string) {
	h.t.Helper()
	if err := h.assertServiceMissing(namespace, name); err != nil {
		h.t.Fatal(err)
	}
}

// AssertSecretCreated verifies that a Secret exists in the fake client.
func (h *FakeHarness) AssertSecretCreated(namespace, name string) {
	h.t.Helper()
	if _, err := h.getSecret(namespace, name); err != nil {
		h.t.Fatal(err)
	}
}

// AssertSecretDeleted verifies that a Secret has been deleted from the fake client.
func (h *FakeHarness) AssertSecretDeleted(namespace, name string) {
	h.t.Helper()
	if err := h.assertSecretMissing(namespace, name); err != nil {
		h.t.Fatal(err)
	}
}

// AssertPVCCreated verifies that a PersistentVolumeClaim exists in the fake client.
func (h *FakeHarness) AssertPVCCreated(namespace, name string) {
	h.t.Helper()
	if _, err := h.getPVC(namespace, name); err != nil {
		h.t.Fatal(err)
	}
}

func (h *FakeHarness) AssertHTTPRouteCreated(namespace, name string) {
	h.t.Helper()
	if _, err := h.getHTTPRoute(namespace, name); err != nil {
		h.t.Fatal(err)
	}
}

func (h *FakeHarness) AssertHTTPRouteUpdated(namespace, name string, expected *unstructured.Unstructured) {
	h.t.Helper()
	stored, err := h.getHTTPRoute(namespace, name)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := assertHTTPRouteMatches(stored, expected); err != nil {
		h.t.Fatal(err)
	}
}

func (h *FakeHarness) AssertHTTPRouteDeleted(namespace, name string) {
	h.t.Helper()
	if err := h.assertHTTPRouteMissing(namespace, name); err != nil {
		h.t.Fatal(err)
	}
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

	return false, nil, nil
}

func (h *FakeHarness) failureFor(resourceType, operation string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.failures[failureKey{resourceType: resourceType, operation: operation}]
}

func (h *FakeHarness) getDeployment(namespace, name string) (*appsv1.Deployment, error) {
	deployment, err := h.typedClient.AppsV1().Deployments(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("deployment %s/%s lookup failed: %w", namespace, name, err)
	}
	return deployment, nil
}

func (h *FakeHarness) getService(namespace, name string) (*corev1.Service, error) {
	service, err := h.typedClient.CoreV1().Services(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("service %s/%s lookup failed: %w", namespace, name, err)
	}
	return service, nil
}

func (h *FakeHarness) getSecret(namespace, name string) (*corev1.Secret, error) {
	secret, err := h.typedClient.CoreV1().Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("secret %s/%s lookup failed: %w", namespace, name, err)
	}
	return secret, nil
}

func (h *FakeHarness) getPVC(namespace, name string) (*corev1.PersistentVolumeClaim, error) {
	pvc, err := h.typedClient.CoreV1().PersistentVolumeClaims(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("pvc %s/%s lookup failed: %w", namespace, name, err)
	}
	return pvc, nil
}

func (h *FakeHarness) getHTTPRoute(namespace, name string) (*unstructured.Unstructured, error) {
	route, err := h.dynamicClient.Resource(httpRouteGVR()).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("httproute %s/%s lookup failed: %w", namespace, name, err)
	}
	return route, nil
}

func (h *FakeHarness) assertDeploymentMissing(namespace, name string) error {
	_, err := h.typedClient.AppsV1().Deployments(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err == nil {
		return fmt.Errorf("deployment %s/%s should have been deleted", namespace, name)
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("deployment %s/%s delete check failed: %w", namespace, name, err)
	}
	return nil
}

func (h *FakeHarness) assertServiceMissing(namespace, name string) error {
	_, err := h.typedClient.CoreV1().Services(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err == nil {
		return fmt.Errorf("service %s/%s should have been deleted", namespace, name)
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("service %s/%s delete check failed: %w", namespace, name, err)
	}
	return nil
}

func (h *FakeHarness) assertSecretMissing(namespace, name string) error {
	_, err := h.typedClient.CoreV1().Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err == nil {
		return fmt.Errorf("secret %s/%s should have been deleted", namespace, name)
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("secret %s/%s delete check failed: %w", namespace, name, err)
	}
	return nil
}

func (h *FakeHarness) assertHTTPRouteMissing(namespace, name string) error {
	_, err := h.dynamicClient.Resource(httpRouteGVR()).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err == nil {
		return fmt.Errorf("httproute %s/%s should have been deleted", namespace, name)
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("httproute %s/%s delete check failed: %w", namespace, name, err)
	}
	return nil
}

func assertDeploymentMatches(stored, expected *appsv1.Deployment) error {
	if stored == nil || expected == nil {
		return fmt.Errorf("deployment comparison requires non-nil objects")
	}
	got := normalizeDeploymentForAssertion(stored)
	want := normalizeDeploymentForAssertion(expected)
	if reflect.DeepEqual(got, want) {
		return nil
	}
	return fmt.Errorf("deployment %s/%s mismatch (-got +want): got=%#v want=%#v", stored.Namespace, stored.Name, got, want)
}

func assertServiceMatches(stored, expected *corev1.Service) error {
	if stored == nil || expected == nil {
		return fmt.Errorf("service comparison requires non-nil objects")
	}
	got := normalizeServiceForAssertion(stored)
	want := normalizeServiceForAssertion(expected)
	if reflect.DeepEqual(got, want) {
		return nil
	}
	return fmt.Errorf("service %s/%s mismatch (-got +want): got=%#v want=%#v", stored.Namespace, stored.Name, got, want)
}

func assertHTTPRouteMatches(stored, expected *unstructured.Unstructured) error {
	if stored == nil || expected == nil {
		return fmt.Errorf("httproute comparison requires non-nil objects")
	}
	got := normalizeHTTPRouteForAssertion(stored)
	want := normalizeHTTPRouteForAssertion(expected)
	if reflect.DeepEqual(got, want) {
		return nil
	}
	return fmt.Errorf("httproute %s/%s mismatch (-got +want): got=%#v want=%#v", stored.GetNamespace(), stored.GetName(), got.Object, want.Object)
}

func normalizeDeploymentForAssertion(deployment *appsv1.Deployment) *appsv1.Deployment {
	normalized := deployment.DeepCopy()
	normalizeObjectMeta(&normalized.ObjectMeta)
	normalizePodTemplateMeta(&normalized.Spec.Template.ObjectMeta)
	return normalized
}

func normalizeServiceForAssertion(service *corev1.Service) *corev1.Service {
	normalized := service.DeepCopy()
	normalizeObjectMeta(&normalized.ObjectMeta)
	normalized.Spec.ClusterIP = ""
	normalized.Spec.ClusterIPs = nil
	normalized.Spec.HealthCheckNodePort = 0
	normalized.Spec.IPFamilies = nil
	normalized.Spec.IPFamilyPolicy = nil
	normalized.Spec.SessionAffinityConfig = nil
	normalized.Status = corev1.ServiceStatus{}
	return normalized
}

func normalizeHTTPRouteForAssertion(route *unstructured.Unstructured) *unstructured.Unstructured {
	normalized := route.DeepCopy()
	unstructured.RemoveNestedField(normalized.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(normalized.Object, "metadata", "generation")
	unstructured.RemoveNestedField(normalized.Object, "metadata", "uid")
	unstructured.RemoveNestedField(normalized.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(normalized.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(normalized.Object, "status")
	return normalized
}

func normalizeObjectMeta(meta *metav1.ObjectMeta) {
	meta.ResourceVersion = ""
	meta.Generation = 0
	meta.UID = ""
	meta.CreationTimestamp = metav1.Time{}
	meta.DeletionTimestamp = nil
	meta.DeletionGracePeriodSeconds = nil
	meta.ManagedFields = nil
	meta.OwnerReferences = nil
	meta.Finalizers = nil
}

func normalizePodTemplateMeta(meta *metav1.ObjectMeta) {
	normalizeObjectMeta(meta)
	meta.Name = ""
	meta.Namespace = ""
	meta.GenerateName = ""
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
	case resourceKindPVC, "pvcs":
		return resourceKindPVC, true
	case resourceKindSecret, "secrets":
		return resourceKindSecret, true
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
