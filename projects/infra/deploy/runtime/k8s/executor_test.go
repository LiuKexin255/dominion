package k8s

import (
	"context"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiRuntime "k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"dominion/projects/infra/deploy/domain"
)

func TestK8sRuntimeApplyCreatesResources(t *testing.T) {
	ctx := context.Background()
	runtime := newTestK8sRuntime(t)
	env := newExecutorTestEnvironment(t)

	if err := runtime.Apply(ctx, env, nil); err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}

	objects, err := ConvertToWorkloads(env, runtime.client.K8sConfig)
	if err != nil {
		t.Fatalf("ConvertToWorkloads() failed: %v", err)
	}

	dep, err := BuildDeployment(objects.Deployments[0], runtime.client.K8sConfig)
	if err != nil {
		t.Fatalf("BuildDeployment() failed: %v", err)
	}
	if _, err := runtime.client.TypedClient.AppsV1().Deployments(dep.Namespace).Get(ctx, dep.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("deployment not created: %v", err)
	}

	svc, err := BuildService(objects.Deployments[0], runtime.client.K8sConfig)
	if err != nil {
		t.Fatalf("BuildService() failed: %v", err)
	}
	if _, err := runtime.client.TypedClient.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("service not created: %v", err)
	}

	route, err := BuildHTTPRoute(objects.HTTPRoutes[0], runtime.client.K8sConfig)
	if err != nil {
		t.Fatalf("BuildHTTPRoute() failed: %v", err)
	}
	if _, err := runtime.client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Get(ctx, route.GetName(), metav1.GetOptions{}); err != nil {
		t.Fatalf("httproute not created: %v", err)
	}

	mongo := objects.MongoDBWorkloads[0]
	if _, err := runtime.client.TypedClient.CoreV1().PersistentVolumeClaims(runtime.client.K8sConfig.Namespace).Get(ctx, mongo.PVCResourceName(), metav1.GetOptions{}); err != nil {
		t.Fatalf("pvc not created: %v", err)
	}
	if _, err := runtime.client.TypedClient.CoreV1().Secrets(runtime.client.K8sConfig.Namespace).Get(ctx, mongo.SecretResourceName(), metav1.GetOptions{}); err != nil {
		t.Fatalf("secret not created: %v", err)
	}
	if _, err := runtime.client.TypedClient.AppsV1().Deployments(runtime.client.K8sConfig.Namespace).Get(ctx, mongo.ResourceName(), metav1.GetOptions{}); err != nil {
		t.Fatalf("mongodb deployment not created: %v", err)
	}
	if _, err := runtime.client.TypedClient.CoreV1().Services(runtime.client.K8sConfig.Namespace).Get(ctx, mongo.ServiceResourceName(), metav1.GetOptions{}); err != nil {
		t.Fatalf("mongodb service not created: %v", err)
	}
}

func TestK8sRuntimeApplyUsesCreateOrUpdate(t *testing.T) {
	ctx := context.Background()
	runtime := newTestK8sRuntime(t)
	env := newExecutorTestEnvironment(t)
	objects, err := ConvertToWorkloads(env, runtime.client.K8sConfig)
	if err != nil {
		t.Fatalf("ConvertToWorkloads() failed: %v", err)
	}

	dep, _ := BuildDeployment(objects.Deployments[0], runtime.client.K8sConfig)
	svc, _ := BuildService(objects.Deployments[0], runtime.client.K8sConfig)
	route, _ := BuildHTTPRoute(objects.HTTPRoutes[0], runtime.client.K8sConfig)
	secret, _ := BuildMongoDBSecret(objects.MongoDBWorkloads[0], runtime.client.K8sConfig)

	dep.ResourceVersion = "11"
	svc.ResourceVersion = "12"
	svc.Spec.ClusterIP = "10.0.0.8"
	route.SetResourceVersion("13")
	secret.ResourceVersion = "14"

	seedTypedObject(t, runtime, dep)
	seedTypedObject(t, runtime, svc)
	seedDynamicObject(t, runtime, route)
	seedTypedObject(t, runtime, secret)

	gotRV := map[string]string{}
	runtime.client.TypedClient.(*kubernetesfake.Clientset).PrependReactor("update", "*", func(action k8stesting.Action) (bool, apiRuntime.Object, error) {
		updateAction, ok := action.(k8stesting.UpdateAction)
		if !ok {
			return false, nil, nil
		}
		obj := updateAction.GetObject()
		switch typed := obj.(type) {
		case *appsv1.Deployment:
			gotRV[typed.Name] = typed.ResourceVersion
		case *corev1.Service:
			gotRV[typed.Name] = typed.ResourceVersion
		case *corev1.Secret:
			gotRV[typed.Name] = typed.ResourceVersion
		}
		return false, nil, nil
	})
	runtime.client.DynamicClient.(*dynamicfake.FakeDynamicClient).PrependReactor("update", "*", func(action k8stesting.Action) (bool, apiRuntime.Object, error) {
		updateAction, ok := action.(k8stesting.UpdateAction)
		if !ok {
			return false, nil, nil
		}
		obj, ok := updateAction.GetObject().(*unstructured.Unstructured)
		if ok {
			gotRV[obj.GetName()] = obj.GetResourceVersion()
		}
		return false, nil, nil
	})

	if err := runtime.Apply(ctx, env, nil); err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}

	if gotRV[dep.Name] != "11" {
		t.Fatalf("deployment resourceVersion = %q, want %q", gotRV[dep.Name], "11")
	}
	if gotRV[svc.Name] != "12" {
		t.Fatalf("service resourceVersion = %q, want %q", gotRV[svc.Name], "12")
	}
	if gotRV[route.GetName()] != "13" {
		t.Fatalf("httproute resourceVersion = %q, want %q", gotRV[route.GetName()], "13")
	}
	if gotRV[secret.Name] != "14" {
		t.Fatalf("secret resourceVersion = %q, want %q", gotRV[secret.Name], "14")
	}
}

func TestK8sRuntimeApplyPrunesOrphanResources(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		seedState    *domain.DesiredState
		desiredState *domain.DesiredState
		wantPresent  pruneResourcePresence
		wantAbsent   pruneResourcePresence
	}{
		{
			name: "remove service prunes deployment and service only",
			seedState: &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					newExecutorTestArtifactSpec("api", "demo", 8080, nil),
					newExecutorTestArtifactSpec("worker", "demo", 9090, nil),
				},
			},
			desiredState: &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					newExecutorTestArtifactSpec("worker", "demo", 9090, nil),
				},
			},
			wantPresent: pruneResourcePresence{
				Deployments: []string{newObjectName(WorkloadKindDeployment, "demo", "worker")},
				Services:    []string{newObjectName(WorkloadKindService, "demo", "worker")},
			},
			wantAbsent: pruneResourcePresence{
				Deployments: []string{newObjectName(WorkloadKindDeployment, "demo", "api")},
				Services:    []string{newObjectName(WorkloadKindService, "demo", "api")},
			},
		},
		{
			name: "remove infra prunes deployment service secret but preserves pvc",
			seedState: &domain.DesiredState{
				Infras: []*domain.InfraSpec{
					newExecutorTestMongoInfraSpec("mongo", "demo"),
					newExecutorTestMongoInfraSpec("mongo-keep", "demo"),
				},
			},
			desiredState: &domain.DesiredState{
				Infras: []*domain.InfraSpec{
					newExecutorTestMongoInfraSpec("mongo-keep", "demo"),
				},
			},
			wantPresent: pruneResourcePresence{
				Deployments: []string{newObjectName(WorkloadKindMongoDB, "demo", "mongo-keep")},
				Services:    []string{newObjectName(WorkloadKindService, "demo", "mongo-keep")},
				Secrets:     []string{newObjectName(WorkloadKindSecret, "demo", "mongo-keep")},
				PVCs:        []string{newObjectName(WorkloadKindPVC, "demo", "mongo"), newObjectName(WorkloadKindPVC, "demo", "mongo-keep")},
			},
			wantAbsent: pruneResourcePresence{
				Deployments: []string{newObjectName(WorkloadKindMongoDB, "demo", "mongo")},
				Services:    []string{newObjectName(WorkloadKindService, "demo", "mongo")},
				Secrets:     []string{newObjectName(WorkloadKindSecret, "demo", "mongo")},
			},
		},
		{
			name: "remove httproute prunes route and preserves backend service",
			seedState: &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					newExecutorTestArtifactSpec("api", "demo", 8080, &domain.ArtifactHTTPSpec{
						Hostnames: []string{"demo.example.com"},
						Matches: []domain.HTTPRouteRule{{
							Backend: "http",
							Path: domain.HTTPPathRule{
								Type:  domain.HTTPPathRuleTypePathPrefix,
								Value: "/",
							},
						}},
					}),
				},
			},
			desiredState: &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					newExecutorTestArtifactSpec("api", "demo", 8080, nil),
				},
			},
			wantPresent: pruneResourcePresence{
				Deployments: []string{newObjectName(WorkloadKindDeployment, "demo", "api")},
				Services:    []string{newObjectName(WorkloadKindService, "demo", "api")},
			},
			wantAbsent: pruneResourcePresence{
				HTTPRoutes: []string{newObjectName(WorkloadKindHTTPRoute, "demo", "api")},
			},
		},
		{
			name: "empty desired state prunes all running resources except pvc",
			seedState: &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					newExecutorTestArtifactSpec("api", "demo", 8080, &domain.ArtifactHTTPSpec{
						Hostnames: []string{"demo.example.com"},
						Matches: []domain.HTTPRouteRule{{
							Backend: "http",
							Path: domain.HTTPPathRule{
								Type:  domain.HTTPPathRuleTypePathPrefix,
								Value: "/",
							},
						}},
					}),
				},
				Infras: []*domain.InfraSpec{
					newExecutorTestMongoInfraSpec("mongo", "demo"),
				},
			},
			desiredState: &domain.DesiredState{},
			wantPresent: pruneResourcePresence{
				PVCs: []string{newObjectName(WorkloadKindPVC, "demo", "mongo")},
			},
			wantAbsent: pruneResourcePresence{
				Deployments: []string{
					newObjectName(WorkloadKindDeployment, "demo", "api"),
					newObjectName(WorkloadKindMongoDB, "demo", "mongo"),
				},
				Services: []string{
					newObjectName(WorkloadKindService, "demo", "api"),
					newObjectName(WorkloadKindService, "demo", "mongo"),
				},
				HTTPRoutes: []string{newObjectName(WorkloadKindHTTPRoute, "demo", "api")},
				Secrets:    []string{newObjectName(WorkloadKindSecret, "demo", "mongo")},
			},
		},
		{
			name: "add new service while pruning old service",
			seedState: &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					newExecutorTestArtifactSpec("old-api", "demo", 8080, nil),
				},
			},
			desiredState: &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					newExecutorTestArtifactSpec("new-api", "demo", 8081, nil),
				},
			},
			wantPresent: pruneResourcePresence{
				Deployments: []string{newObjectName(WorkloadKindDeployment, "demo", "new-api")},
				Services:    []string{newObjectName(WorkloadKindService, "demo", "new-api")},
			},
			wantAbsent: pruneResourcePresence{
				Deployments: []string{newObjectName(WorkloadKindDeployment, "demo", "old-api")},
				Services:    []string{newObjectName(WorkloadKindService, "demo", "old-api")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := newTestK8sRuntime(t)
			seedEnv := newExecutorTestEnvironmentWithState(t, tt.seedState)
			if err := runtime.Apply(ctx, seedEnv, nil); err != nil {
				t.Fatalf("seed Apply() failed: %v", err)
			}

			desiredEnv := newExecutorTestEnvironmentWithState(t, tt.desiredState)
			if err := runtime.Apply(ctx, desiredEnv, nil); err != nil {
				t.Fatalf("Apply() failed: %v", err)
			}

			assertPruneResourcePresence(t, runtime, tt.wantPresent)
			assertPruneResourceAbsence(t, runtime, tt.wantAbsent)
		})
	}
}

func TestK8sRuntimeDeleteDeletesOwnedResourcesInFixedOrder(t *testing.T) {
	ctx := context.Background()
	runtime := newTestK8sRuntime(t)
	env := newExecutorTestEnvironment(t)
	objects, err := ConvertToWorkloads(env, runtime.client.K8sConfig)
	if err != nil {
		t.Fatalf("ConvertToWorkloads() failed: %v", err)
	}
	fullEnvName := env.Name().Label()

	route, _ := BuildHTTPRoute(objects.HTTPRoutes[0], runtime.client.K8sConfig)
	svc, _ := BuildService(objects.Deployments[0], runtime.client.K8sConfig)
	dep, _ := BuildDeployment(objects.Deployments[0], runtime.client.K8sConfig)
	secret, _ := BuildMongoDBSecret(objects.MongoDBWorkloads[0], runtime.client.K8sConfig)
	pvc, _ := BuildMongoDBPVC(objects.MongoDBWorkloads[0], runtime.client.K8sConfig)

	seedDynamicObject(t, runtime, route)
	seedTypedObject(t, runtime, svc)
	seedTypedObject(t, runtime, dep)
	seedTypedObject(t, runtime, secret)
	seedTypedObject(t, runtime, pvc)

	unownedService := svc.DeepCopy()
	unownedService.Name = "svc-unowned"
	unownedService.Labels = map[string]string{managedByLabelKey: runtime.client.K8sConfig.ManagedBy}
	seedTypedObject(t, runtime, unownedService)

	var deleteOrder []string
	runtime.client.DynamicClient.(*dynamicfake.FakeDynamicClient).PrependReactor("delete", "httproutes", func(action k8stesting.Action) (bool, apiRuntime.Object, error) {
		deleteOrder = append(deleteOrder, resourceKindHTTPRoute)
		return false, nil, nil
	})
	runtime.client.TypedClient.(*kubernetesfake.Clientset).PrependReactor("delete", "services", func(action k8stesting.Action) (bool, apiRuntime.Object, error) {
		deleteOrder = append(deleteOrder, resourceKindService)
		return false, nil, nil
	})
	runtime.client.TypedClient.(*kubernetesfake.Clientset).PrependReactor("delete", "deployments", func(action k8stesting.Action) (bool, apiRuntime.Object, error) {
		deleteOrder = append(deleteOrder, resourceKindDeployment)
		return false, nil, nil
	})
	runtime.client.TypedClient.(*kubernetesfake.Clientset).PrependReactor("delete", "secrets", func(action k8stesting.Action) (bool, apiRuntime.Object, error) {
		deleteOrder = append(deleteOrder, resourceKindSecret)
		return false, nil, nil
	})

	if err := runtime.Delete(ctx, env.Name()); err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}

	wantOrder := []string{resourceKindHTTPRoute, resourceKindService, resourceKindDeployment, resourceKindSecret}
	if !reflect.DeepEqual(deleteOrder, wantOrder) {
		t.Fatalf("delete order = %v, want %v", deleteOrder, wantOrder)
	}

	_, err = runtime.client.DynamicClient.Resource(httpRouteGVR()).Namespace(route.GetNamespace()).Get(ctx, route.GetName(), metav1.GetOptions{})
	assertNotFound(t, err)
	_, err = runtime.client.TypedClient.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
	assertNotFound(t, err)
	_, err = runtime.client.TypedClient.AppsV1().Deployments(dep.Namespace).Get(ctx, dep.Name, metav1.GetOptions{})
	assertNotFound(t, err)
	_, err = runtime.client.TypedClient.CoreV1().Secrets(secret.Namespace).Get(ctx, secret.Name, metav1.GetOptions{})
	assertNotFound(t, err)

	if _, err := runtime.client.TypedClient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Get(ctx, pvc.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("pvc should be preserved: %v", err)
	}
	if _, err := runtime.client.TypedClient.CoreV1().Services(unownedService.Namespace).Get(ctx, unownedService.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("unowned service should be preserved: %v", err)
	}

	selector := buildLabelSelector(buildLabels(withDominionEnvironment(fullEnvName), withManagedBy(runtime.client.K8sConfig.ManagedBy)))
	if selector == "" {
		t.Fatal("label selector should not be empty")
	}
}

func newExecutorTestEnvironment(t *testing.T) *domain.Environment {
	t.Helper()

	return newExecutorTestEnvironmentWithState(t, &domain.DesiredState{
		Artifacts: []*domain.ArtifactSpec{{
			Name:       "api",
			App:        "demo",
			Image:      "repo/demo:v1",
			Replicas:   2,
			TLSEnabled: true,
			Ports:      []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
			HTTP: &domain.ArtifactHTTPSpec{
				Hostnames: []string{"demo.example.com"},
				Matches: []domain.HTTPRouteRule{{
					Backend: "http",
					Path: domain.HTTPPathRule{
						Type:  domain.HTTPPathRuleTypePathPrefix,
						Value: "/",
					},
				}},
			},
		}},
		Infras: []*domain.InfraSpec{{
			Resource: infraResourceMongoDB,
			Profile:  "dev-single",
			Name:     "mongo",
			App:      "demo",
			Persistence: domain.InfraPersistenceSpec{
				Enabled: true,
			},
		}},
	})
}

func newExecutorTestEnvironmentWithState(t *testing.T, state *domain.DesiredState) *domain.Environment {
	t.Helper()
	envName, err := domain.NewEnvironmentName("tstscope", "dev")
	if err != nil {
		t.Fatalf("NewEnvironmentName() failed: %v", err)
	}

	env, err := domain.NewEnvironment(envName, domain.EnvironmentTypeProd, "executor test environment", state)
	if err != nil {
		t.Fatalf("NewEnvironment() failed: %v", err)
	}

	return env
}

type pruneResourcePresence struct {
	Deployments []string
	Services    []string
	HTTPRoutes  []string
	Secrets     []string
	PVCs        []string
}

func newExecutorTestArtifactSpec(name string, app string, port int32, http *domain.ArtifactHTTPSpec) *domain.ArtifactSpec {
	return &domain.ArtifactSpec{
		Name:       name,
		App:        app,
		Image:      "repo/" + app + ":v1",
		Replicas:   1,
		TLSEnabled: true,
		Ports:      []domain.ArtifactPortSpec{{Name: "http", Port: port}},
		HTTP:       http,
	}
}

func newExecutorTestMongoInfraSpec(name string, app string) *domain.InfraSpec {
	return &domain.InfraSpec{
		Resource: infraResourceMongoDB,
		Profile:  "dev-single",
		Name:     name,
		App:      app,
		Persistence: domain.InfraPersistenceSpec{
			Enabled: true,
		},
	}
}

func assertPruneResourcePresence(t *testing.T, runtime *K8sRuntime, want pruneResourcePresence) {
	t.Helper()
	ctx := context.Background()
	namespace := runtime.client.K8sConfig.Namespace

	for _, name := range want.Deployments {
		if _, err := runtime.client.TypedClient.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{}); err != nil {
			t.Fatalf("deployment %s should exist: %v", name, err)
		}
	}
	for _, name := range want.Services {
		if _, err := runtime.client.TypedClient.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{}); err != nil {
			t.Fatalf("service %s should exist: %v", name, err)
		}
	}
	for _, name := range want.HTTPRoutes {
		if _, err := runtime.client.DynamicClient.Resource(httpRouteGVR()).Namespace(namespace).Get(ctx, name, metav1.GetOptions{}); err != nil {
			t.Fatalf("httproute %s should exist: %v", name, err)
		}
	}
	for _, name := range want.Secrets {
		if _, err := runtime.client.TypedClient.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{}); err != nil {
			t.Fatalf("secret %s should exist: %v", name, err)
		}
	}
	for _, name := range want.PVCs {
		if _, err := runtime.client.TypedClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{}); err != nil {
			t.Fatalf("pvc %s should exist: %v", name, err)
		}
	}
}

func assertPruneResourceAbsence(t *testing.T, runtime *K8sRuntime, want pruneResourcePresence) {
	t.Helper()
	ctx := context.Background()
	namespace := runtime.client.K8sConfig.Namespace

	for _, name := range want.Deployments {
		_, err := runtime.client.TypedClient.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		assertNotFound(t, err)
	}
	for _, name := range want.Services {
		_, err := runtime.client.TypedClient.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		assertNotFound(t, err)
	}
	for _, name := range want.HTTPRoutes {
		_, err := runtime.client.DynamicClient.Resource(httpRouteGVR()).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		assertNotFound(t, err)
	}
	for _, name := range want.Secrets {
		_, err := runtime.client.TypedClient.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		assertNotFound(t, err)
	}
}

func newTestK8sRuntime(t *testing.T) *K8sRuntime {
	t.Helper()
	scheme := apiRuntime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add apps scheme failed: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme failed: %v", err)
	}
	if err := gatewayv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add gateway scheme failed: %v", err)
	}

	config := newExecutorTestConfig()
	typedClient := kubernetesfake.NewSimpleClientset()
	typedClient.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, apiRuntime.Object, error) {
		name := action.(k8stesting.GetAction).GetName()
		obj, err := typedClient.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), config.Namespace, name)
		if err != nil {
			return false, nil, nil
		}
		dep := obj.(*appsv1.Deployment)
		replicas := deploymentSpecReplicas(dep)
		dep.Status = appsv1.DeploymentStatus{
			ObservedGeneration: dep.Generation,
			UpdatedReplicas:    replicas,
			AvailableReplicas:  replicas,
		}
		return true, dep, nil
	})

	return NewK8sRuntime(&RuntimeClient{
		TypedClient:   typedClient,
		DynamicClient: dynamicfake.NewSimpleDynamicClient(scheme),
		K8sConfig:     config,
	})
}

func newExecutorTestConfig() *K8sConfig {
	return &K8sConfig{
		Namespace: "test-ns",
		ManagedBy: "dominion.io",
		Gateway: GatewayConfig{
			Name:      "test-gateway",
			Namespace: "ingress",
		},
		TLS: TLSConfig{
			Secret: "test-tls-secret",
			Domain: "test.example.com",
			CAConfigMap: ConfigMapConfig{
				Name: "test-ca",
				Key:  "ca.crt",
			},
		},
		MongoDB: map[string]*MongoProfileConfig{
			"dev-single": {
				Image:         "mongo",
				Version:       "7.0",
				Port:          27017,
				AdminUsername: "admin",
				Security: MongoSecurityConfig{
					RunAsUser:  1000,
					RunAsGroup: 3000,
				},
				Storage: MongoStorageConfig{
					StorageClassName: "local-path",
					Capacity:         "1Gi",
					AccessModes:      []string{"ReadWriteOnce"},
					VolumeMode:       "Filesystem",
				},
			},
		},
	}
}

func seedTypedObject(t *testing.T, runtime *K8sRuntime, obj apiRuntime.Object) {
	t.Helper()
	if err := runtime.client.TypedClient.(*kubernetesfake.Clientset).Tracker().Add(obj); err != nil {
		t.Fatalf("seed typed object failed: %v", err)
	}
}

func seedDynamicObject(t *testing.T, runtime *K8sRuntime, obj *unstructured.Unstructured) {
	t.Helper()
	if err := runtime.client.DynamicClient.(*dynamicfake.FakeDynamicClient).Tracker().Add(obj); err != nil {
		t.Fatalf("seed dynamic object failed: %v", err)
	}
}

func assertNotFound(t *testing.T, err error) {
	t.Helper()
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected not found error, got %v", err)
	}
}
