package k8s

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"dominion/tools/deploy/pkg/config"

	corev1 "k8s.io/api/core/v1"
	apiRuntime "k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
)

const (
	mongoIntegrationDeployDirName  = "apps/mongo-integration"
	mongoIntegrationDeployFileName = "deploy.yaml"
	mongoIntegrationDeployYAML     = `template: deploy
app: grpc-hello-world
desc: "mongodb integration"
services:
  - infra:
      resource: mongodb
      profile: dev-single
      name: mongo-main
      persistence:
        enabled: true
`
	mongoIntegrationExpectedPassword = "mLXzsNTqMpIxnHGflaKQhcwZUW1MIwOj"
)

func TestEndToEnd_MongoDB_Apply_CreatesResourcesInOrder(t *testing.T) {
	// given
	st := newMongoIntegrationState(t)
	workload := st.objects.MongoDBWorkloads[0]

	wantSecret, err := BuildMongoDBSecret(workload)
	if err != nil {
		t.Fatalf("BuildMongoDBSecret() failed: %v", err)
	}

	var createOrder []string
	st.h.typedClient.PrependReactor("create", "*", func(action k8stesting.Action) (bool, apiRuntime.Object, error) {
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
	if err := st.executor.Apply(context.Background(), st.objects); err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}

	// then
	if len(st.objects.MongoDBWorkloads) != 1 {
		t.Fatalf("mongodb workload count = %d, want 1", len(st.objects.MongoDBWorkloads))
	}
	if !workload.Persistence.Enabled {
		t.Fatal("mongo workload persistence should be enabled")
	}

	wantOrder := []string{resourceKindPVC, resourceKindSecret, resourceKindDeployment, resourceKindService}
	if !reflect.DeepEqual(createOrder, wantOrder) {
		t.Fatalf("create order = %v, want %v", createOrder, wantOrder)
	}

	st.h.AssertPVCCreated("team-dev", workload.PVCResourceName())
	st.h.AssertSecretCreated("team-dev", workload.SecretResourceName())
	st.h.AssertDeploymentCreated("team-dev", workload.ResourceName())
	st.h.AssertServiceCreated("team-dev", workload.ServiceResourceName())

	storedSecret, err := st.h.getSecret("team-dev", workload.SecretResourceName())
	if err != nil {
		t.Fatalf("getSecret() failed: %v", err)
	}
	if string(storedSecret.Data[mongoSecretUsernameKey]) != string(wantSecret.Data[mongoSecretUsernameKey]) {
		t.Fatalf("stored username = %q, want %q", string(storedSecret.Data[mongoSecretUsernameKey]), string(wantSecret.Data[mongoSecretUsernameKey]))
	}
	wantPassword := generateStablePassword(workload.App, workload.EnvironmentName, workload.ServiceName)
	if string(storedSecret.Data[mongoSecretPasswordKey]) != wantPassword {
		t.Fatalf("stored password = %q, want %q", string(storedSecret.Data[mongoSecretPasswordKey]), wantPassword)
	}
}

func TestEndToEnd_MongoDB_Delete_PreservesPVC(t *testing.T) {
	// given
	st := newMongoIntegrationState(t)
	workload := st.objects.MongoDBWorkloads[0]

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
	st.h.SeedPVC(pvc)
	st.h.SeedSecret(secret)
	st.h.SeedDeployment(deployment)
	st.h.SeedService(service)

	// when
	if err := st.executor.Delete(context.Background(), workload.App, workload.EnvironmentName); err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}

	// then
	st.h.AssertDeploymentDeleted(deployment.Namespace, deployment.Name)
	st.h.AssertServiceDeleted(service.Namespace, service.Name)
	st.h.AssertSecretDeleted(secret.Namespace, secret.Name)
	st.h.AssertPVCCreated(pvc.Namespace, pvc.Name)
}

func TestEndToEnd_MongoDB_PVC_Adopt_Compatible(t *testing.T) {
	// given
	st := newMongoIntegrationState(t)
	workload := st.objects.MongoDBWorkloads[0]
	existingPVC := newCompatibleMongoPVC()
	st.h.SeedPVC(existingPVC)

	var createOrder []string
	st.h.typedClient.PrependReactor("create", "*", func(action k8stesting.Action) (bool, apiRuntime.Object, error) {
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
	if err := st.executor.Apply(context.Background(), st.objects); err != nil {
		t.Fatalf("Apply() failed: %v", err)
	}

	// then
	wantOrder := []string{resourceKindSecret, resourceKindDeployment, resourceKindService}
	if !reflect.DeepEqual(createOrder, wantOrder) {
		t.Fatalf("create order = %v, want %v", createOrder, wantOrder)
	}

	st.h.AssertPVCCreated(existingPVC.Namespace, existingPVC.Name)
	st.h.AssertSecretCreated("team-dev", workload.SecretResourceName())
	st.h.AssertDeploymentCreated("team-dev", workload.ResourceName())
	st.h.AssertServiceCreated("team-dev", workload.ServiceResourceName())
}

func TestEndToEnd_MongoDB_PVC_Abort_Incompatible(t *testing.T) {
	// given
	st := newMongoIntegrationState(t)
	workload := st.objects.MongoDBWorkloads[0]
	incompatiblePVC := newMongoPVCWithMutation(func(pvc *corev1.PersistentVolumeClaim) {
		storageClassName := "remote-path"
		pvc.Spec.StorageClassName = &storageClassName
	})
	st.h.SeedPVC(incompatiblePVC)

	// when
	err := st.executor.Apply(context.Background(), st.objects)

	// then
	if err == nil {
		t.Fatal("Apply() expected error")
	}
	if !strings.Contains(err.Error(), workload.PVCResourceName()) {
		t.Fatalf("error = %v, want contains pvc name %q", err, workload.PVCResourceName())
	}
	if !strings.Contains(err.Error(), "storageClassName 不兼容") {
		t.Fatalf("error = %v, want specific storageClassName incompatibility", err)
	}

	st.h.AssertPVCCreated("team-dev", workload.PVCResourceName())
	st.h.AssertSecretDeleted("team-dev", workload.SecretResourceName())
	st.h.AssertDeploymentDeleted("team-dev", workload.ResourceName())
	st.h.AssertServiceDeleted("team-dev", workload.ServiceResourceName())
}

func TestEndToEnd_MongoDB_Password_Consistency(t *testing.T) {
	// given
	st := newMongoIntegrationState(t)
	workload := st.objects.MongoDBWorkloads[0]

	// when
	got := generateStablePassword(workload.App, workload.EnvironmentName, workload.ServiceName)

	// then
	if got != mongoIntegrationExpectedPassword {
		t.Fatalf("generateStablePassword() = %q, want %q", got, mongoIntegrationExpectedPassword)
	}

	secret, err := BuildMongoDBSecret(workload)
	if err != nil {
		t.Fatalf("BuildMongoDBSecret() failed: %v", err)
	}
	if string(secret.Data[mongoSecretPasswordKey]) != mongoIntegrationExpectedPassword {
		t.Fatalf("secret password = %q, want %q", string(secret.Data[mongoSecretPasswordKey]), mongoIntegrationExpectedPassword)
	}
}

type mongoIntegrationState struct {
	deployConfig *config.DeployConfig
	objects      *DeployObjects
	h            *FakeHarness
	executor     *Executor
}

func newMongoIntegrationState(t *testing.T) *mongoIntegrationState {
	t.Helper()

	stubLoadK8sConfig(t, newTestK8sConfigWithMongoProfile())
	deployConfig := mustParseMongoIntegrationDeployConfig(t)
	objects, err := NewDeployObjects(deployConfig, nil, "dev", "grpc-hello-world", nil)
	if err != nil {
		t.Fatalf("NewDeployObjects() failed: %v", err)
	}
	h := NewFakeHarness(t)

	return &mongoIntegrationState{
		deployConfig: deployConfig,
		objects:      objects,
		h:            h,
		executor:     NewExecutor(h.RuntimeClient()),
	}
}

func mustParseMongoIntegrationDeployConfig(t *testing.T) *config.DeployConfig {
	t.Helper()

	workspaceRoot := t.TempDir()
	deployDir := filepath.Join(workspaceRoot, filepath.FromSlash(mongoIntegrationDeployDirName))
	if err := os.MkdirAll(deployDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, "MODULE.bazel"), []byte("module(name = \"mongo_integration_test\")\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(MODULE.bazel) failed: %v", err)
	}
	deployPath := filepath.Join(deployDir, mongoIntegrationDeployFileName)
	if err := os.WriteFile(deployPath, []byte(mongoIntegrationDeployYAML), 0o644); err != nil {
		t.Fatalf("WriteFile(deploy.yaml) failed: %v", err)
	}

	withMongoIntegrationWorkingDir(t, deployDir)

	deployConfig, err := config.ParseDeployConfig(deployPath)
	if err != nil {
		t.Fatalf("ParseDeployConfig() failed: %v", err)
	}
	if len(deployConfig.Services) != 1 {
		t.Fatalf("service count = %d, want 1", len(deployConfig.Services))
	}
	deployConfig.Services[0].Infra.Resource = deployInfraResourceMongoDB

	return deployConfig
}

func withMongoIntegrationWorkingDir(t *testing.T, dir string) {
	t.Helper()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore working dir failed: %v", err)
		}
	})
}
