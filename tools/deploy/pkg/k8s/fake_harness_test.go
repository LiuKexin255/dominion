package k8s

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
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
	if gotDep.Name != dep.Name || gotDep.Namespace != dep.Namespace || gotDep.Labels["app"] != dep.Labels["app"] {
		t.Fatalf("deployment = %+v, want %+v", gotDep.ObjectMeta, dep.ObjectMeta)
	}

	gotSvc, err := client.TypedClient.CoreV1().Services(svc.Namespace).Get(context.Background(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get service failed: %v", err)
	}
	if gotSvc.Name != svc.Name || gotSvc.Namespace != svc.Namespace || gotSvc.Labels["app"] != svc.Labels["app"] {
		t.Fatalf("service = %+v, want %+v", gotSvc.ObjectMeta, svc.ObjectMeta)
	}

	gotRoute, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Get(context.Background(), route.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get httproute failed: %v", err)
	}
	if gotRoute.GetName() != route.GetName() || gotRoute.GetNamespace() != route.GetNamespace() {
		t.Fatalf("httproute = %s/%s, want %s/%s", gotRoute.GetNamespace(), gotRoute.GetName(), route.GetNamespace(), route.GetName())
	}
	wantApp := route.Object["metadata"].(map[string]any)["labels"].(map[string]any)["app"].(string)
	labels, found, err := unstructured.NestedStringMap(gotRoute.Object, "metadata", "labels")
	if err != nil {
		t.Fatalf("read httproute labels failed: %v", err)
	}
	if !found || labels["app"] != wantApp {
		t.Fatalf("httproute labels = %#v, want app=%q", labels, wantApp)
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
			gotDep, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Get(ctx, dep.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get deployment failed: %v", err)
			}
			if gotDep.Name != dep.Name || gotDep.Namespace != dep.Namespace || gotDep.Labels["app"] != dep.Labels["app"] {
				t.Fatalf("deployment = %+v, want %+v", gotDep.ObjectMeta, dep.ObjectMeta)
			}
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
			gotDep, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Get(ctx, dep.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get deployment failed: %v", err)
			}
			if gotDep.Labels["app"] != dep.Labels["app"] || gotDep.Labels["version"] != "v2" {
				t.Fatalf("deployment labels = %#v, want app=%q version=%q", gotDep.Labels, dep.Labels["app"], "v2")
			}
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
			if _, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Get(ctx, dep.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				t.Fatalf("get deployment error = %v, want not found", err)
			}
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
			gotSvc, err := client.TypedClient.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get service failed: %v", err)
			}
			if gotSvc.Name != svc.Name || gotSvc.Namespace != svc.Namespace || gotSvc.Labels["app"] != svc.Labels["app"] {
				t.Fatalf("service = %+v, want %+v", gotSvc.ObjectMeta, svc.ObjectMeta)
			}
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
			gotSvc, err := client.TypedClient.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get service failed: %v", err)
			}
			if gotSvc.Labels["app"] != svc.Labels["app"] || gotSvc.Labels["version"] != "v2" {
				t.Fatalf("service labels = %#v, want app=%q version=%q", gotSvc.Labels, svc.Labels["app"], "v2")
			}
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
			if _, err := client.TypedClient.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				t.Fatalf("get service error = %v, want not found", err)
			}
		})
	}
}

func TestFakeHarness_SecretCreatedAndDeleted(t *testing.T) {
	tests := []struct {
		name       string
		namespace  string
		secretName string
	}{
		{name: "default namespace", namespace: "default", secretName: "app1-secret"},
		{name: "custom namespace", namespace: "team-a", secretName: "gateway-secret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewFakeHarness(t)
			client := h.RuntimeClient()
			ctx := context.Background()
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: tt.namespace,
					Name:      tt.secretName,
					Labels: map[string]string{
						"app": tt.secretName,
					},
				},
				StringData: map[string]string{
					"token": "value",
				},
			}

			h.SeedSecret(secret)
			h.AssertSecretCreated(secret.Namespace, secret.Name)

			if _, err := client.TypedClient.CoreV1().Secrets(secret.Namespace).Get(ctx, secret.Name, metav1.GetOptions{}); err != nil {
				t.Fatalf("get secret failed: %v", err)
			}

			if err := client.TypedClient.CoreV1().Secrets(secret.Namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil {
				t.Fatalf("delete secret failed: %v", err)
			}
			h.AssertSecretDeleted(secret.Namespace, secret.Name)
			if _, err := client.TypedClient.CoreV1().Secrets(secret.Namespace).Get(ctx, secret.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				t.Fatalf("get secret error = %v, want not found", err)
			}
		})
	}
}

func TestFakeHarness_PVCCreated(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		pvcName   string
	}{
		{name: "default namespace", namespace: "default", pvcName: "app1-pvc"},
		{name: "custom namespace", namespace: "team-a", pvcName: "gateway-pvc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewFakeHarness(t)
			client := h.RuntimeClient()
			ctx := context.Background()
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: tt.namespace,
					Name:      tt.pvcName,
					Labels: map[string]string{
						"app": tt.pvcName,
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
				},
			}

			h.SeedPVC(pvc)
			h.AssertPVCCreated(pvc.Namespace, pvc.Name)

			gotPVC, err := client.TypedClient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Get(ctx, pvc.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get pvc failed: %v", err)
			}
			if gotPVC.Name != pvc.Name || gotPVC.Namespace != pvc.Namespace || gotPVC.Labels["app"] != pvc.Labels["app"] {
				t.Fatalf("pvc = %+v, want %+v", gotPVC.ObjectMeta, pvc.ObjectMeta)
			}
		})
	}
}

func TestNormalizeResourceTypeFromAction(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		want     string
		ok       bool
	}{
		{name: "deployment singular", resource: "deployment", want: resourceKindDeployment, ok: true},
		{name: "service plural", resource: "services", want: resourceKindService, ok: true},
		{name: "httproute plural", resource: "httproutes", want: resourceKindHTTPRoute, ok: true},
		{name: "pvc singular", resource: "pvc", want: resourceKindPVC, ok: true},
		{name: "pvc plural", resource: "pvcs", want: resourceKindPVC, ok: true},
		{name: "secret singular", resource: "secret", want: resourceKindSecret, ok: true},
		{name: "secret plural", resource: "secrets", want: resourceKindSecret, ok: true},
		{name: "unknown", resource: "configmaps", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeResourceTypeFromAction(tt.resource)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("got = %q, want %q", got, tt.want)
			}
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
			gotRoute, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Get(ctx, route.GetName(), metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get httproute failed: %v", err)
			}
			wantApp := route.Object["metadata"].(map[string]any)["labels"].(map[string]any)["app"].(string)
			labels, found, err := unstructured.NestedStringMap(gotRoute.Object, "metadata", "labels")
			if err != nil {
				t.Fatalf("read httproute labels failed: %v", err)
			}
			if !found || labels["app"] != wantApp {
				t.Fatalf("httproute labels = %#v, want app=%q", labels, wantApp)
			}
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
			gotRoute, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Get(ctx, route.GetName(), metav1.GetOptions{})
			if err != nil {
				t.Fatalf("get httproute failed: %v", err)
			}
			wantApp := route.Object["metadata"].(map[string]any)["labels"].(map[string]any)["app"].(string)
			labels, found, err := unstructured.NestedStringMap(gotRoute.Object, "metadata", "labels")
			if err != nil {
				t.Fatalf("read httproute labels failed: %v", err)
			}
			if !found || labels["app"] != wantApp || labels["version"] != "v2" {
				t.Fatalf("httproute labels = %#v, want app=%q version=%q", labels, wantApp, "v2")
			}
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
			if _, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Get(ctx, route.GetName(), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				t.Fatalf("get httproute error = %v, want not found", err)
			}
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

	gotDep, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Get(ctx, dep.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment failed: %v", err)
	}
	if gotDep.Labels["app"] != dep.Labels["app"] {
		t.Fatalf("deployment labels = %#v, want app=%q", gotDep.Labels, dep.Labels["app"])
	}

	gotSvc, err := client.TypedClient.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get service failed: %v", err)
	}
	if gotSvc.Labels["app"] != svc.Labels["app"] {
		t.Fatalf("service labels = %#v, want app=%q", gotSvc.Labels, svc.Labels["app"])
	}

	gotRoute, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Get(ctx, route.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get httproute failed: %v", err)
	}
	wantApp := route.Object["metadata"].(map[string]any)["labels"].(map[string]any)["app"].(string)
	labels, found, err := unstructured.NestedStringMap(gotRoute.Object, "metadata", "labels")
	if err != nil {
		t.Fatalf("read httproute labels failed: %v", err)
	}
	if !found || labels["app"] != wantApp {
		t.Fatalf("httproute labels = %#v, want app=%q", labels, wantApp)
	}
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

	gotDep, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Get(ctx, dep.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment failed: %v", err)
	}
	if gotDep.Labels["sequence"] != "1" {
		t.Fatalf("deployment labels = %#v, want sequence=%q", gotDep.Labels, "1")
	}

	gotSvc, err := client.TypedClient.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get service failed: %v", err)
	}
	if gotSvc.Labels["sequence"] != "1" {
		t.Fatalf("service labels = %#v, want sequence=%q", gotSvc.Labels, "1")
	}

	gotRoute, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Get(ctx, route.GetName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get httproute failed: %v", err)
	}
	labels, found, err := unstructured.NestedStringMap(gotRoute.Object, "metadata", "labels")
	if err != nil {
		t.Fatalf("read httproute labels failed: %v", err)
	}
	if !found || labels["sequence"] != "1" {
		t.Fatalf("httproute labels = %#v, want sequence=%q", labels, "1")
	}
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

	if _, err := client.TypedClient.AppsV1().Deployments(dep.Namespace).Get(ctx, dep.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("get deployment error = %v, want not found", err)
	}
	if _, err := client.TypedClient.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("get service error = %v, want not found", err)
	}
	if _, err := client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Get(ctx, route.GetName(), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("get httproute error = %v, want not found", err)
	}
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
