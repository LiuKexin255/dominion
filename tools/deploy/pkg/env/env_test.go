package env

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"dominion/tools/deploy/pkg/config"
	"dominion/tools/deploy/pkg/k8s"
)

func copyDir(t *testing.T, src string, dst string) {
	t.Helper()

	err := filepath.Walk(src, func(srcPath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(src, srcPath)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(dstPath, os.ModePerm)
		}

		raw, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), os.ModePerm); err != nil {
			return err
		}

		return os.WriteFile(dstPath, raw, info.Mode())
	})
	if err != nil {
		t.Fatalf("copyDir() failed: %v", err)
	}
}

func newBazelWorkspace(t *testing.T) string {
	t.Helper()

	srcRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	copyDir(t, filepath.Join(srcRoot, "testdata", "service"), filepath.Join(root, "service"))
	copyDir(t, filepath.Join(srcRoot, "testdata", "gateway"), filepath.Join(root, "gateway"))

	for _, dir := range []string{
		filepath.Join(root, cacheDir),
		filepath.Join(root, profileDir),
		filepath.Join(root, deployConfigDir),
		filepath.Join(root, serviceConfigDir),
	} {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			t.Fatalf("os.MkdirAll(%q) failed: %v", dir, err)
		}
	}

	withWorkingDir(t, root)
	return root
}

func withWorkingDir(t *testing.T, dir string) {
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

func testFullEnv(scope string, name string) FullEnvName {
	return FullEnvName(scope + "." + name)
}

func withNow(t *testing.T, fixed time.Time) {
	t.Helper()

	oldNow := now
	now = func() time.Time {
		return fixed
	}
	t.Cleanup(func() {
		now = oldNow
	})
}

type stubExecutor struct {
	applyFunc  func(ctx context.Context, objects *k8s.DeployObjects) error
	deleteFunc func(ctx context.Context, environment string) error
}

func (e *stubExecutor) Apply(ctx context.Context, objects *k8s.DeployObjects) error {
	if e == nil || e.applyFunc == nil {
		return nil
	}
	return e.applyFunc(ctx, objects)
}

func (e *stubExecutor) Delete(ctx context.Context, environment string) error {
	if e == nil || e.deleteFunc == nil {
		return nil
	}
	return e.deleteFunc(ctx, environment)
}

type stubImageResolver struct {
	resolveFunc func(ctx context.Context, deployConfig *config.DeployConfig, serviceConfigs []*config.ServiceConfig) (map[string]string, error)
}

func (r *stubImageResolver) Resolve(ctx context.Context, deployConfig *config.DeployConfig, serviceConfigs []*config.ServiceConfig) (map[string]string, error) {
	if r == nil || r.resolveFunc == nil {
		return nil, nil
	}
	return r.resolveFunc(ctx, deployConfig, serviceConfigs)
}

func withImageResolver(t *testing.T, resolver imageResolver) {
	t.Helper()

	oldNewImageResolver := newImageResolver
	newImageResolver = func() imageResolver {
		return resolver
	}
	t.Cleanup(func() {
		newImageResolver = oldNewImageResolver
	})
}

func TestDeployContext_SaveLoad(t *testing.T) {
	root := newBazelWorkspace(t)
	internalInit()

	want := &DeployContext{
		ActiveEnv:    testFullEnv("alice", "dev"),
		DefaultScope: "alice",
	}

	if err := want.Save(); err != nil {
		t.Fatalf("saveDeployContext() failed: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, currentEnvPath))
	if err != nil {
		t.Fatalf("os.ReadFile() failed: %v", err)
	}
	gotJSON := string(raw)
	if !strings.Contains(gotJSON, `"active_env":"alice.dev"`) {
		t.Fatalf("saved json = %s, want active_env string", gotJSON)
	}
	if !strings.Contains(gotJSON, `"default_scope":"alice"`) {
		t.Fatalf("saved json = %s, want default_scope field", gotJSON)
	}

	got, err := LoadDeployContext()
	if err != nil {
		t.Fatalf("loadDeployContext() failed: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("loadDeployContext() = %v, want %v", got, want)
	}
}

func TestDeployContext_LoadMissingFile(t *testing.T) {
	newBazelWorkspace(t)
	internalInit()

	got, err := LoadDeployContext()
	if err != nil {
		t.Fatalf("loadDeployContext() failed: %v", err)
	}

	if !reflect.DeepEqual(got, &DeployContext{}) {
		t.Fatalf("loadDeployContext() = %v, want empty context", got)
	}
}

func TestDeployContext_LoadCorruptJSON(t *testing.T) {
	root := newBazelWorkspace(t)
	internalInit()

	if err := os.WriteFile(filepath.Join(root, currentEnvPath), []byte("{broken json"), os.ModePerm); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}

	_, err := LoadDeployContext()
	if err == nil {
		t.Fatal("loadDeployContext() succeeded unexpectedly")
	}
	if !strings.Contains(err.Error(), "load deploy context") {
		t.Fatalf("loadDeployContext() error = %v, want context error", err)
	}
}

func TestDeployContext_DefaultScopeAccessors(t *testing.T) {
	ctx := &DeployContext{}
	if got := ctx.GetDefaultScope(); got != "" {
		t.Fatalf("GetDefaultScope() = %q, want empty", got)
	}

	if err := ctx.SetDefaultScope("alice"); err != nil {
		t.Fatalf("SetDefaultScope() failed: %v", err)
	}
	if got := ctx.GetDefaultScope(); got != "alice" {
		t.Fatalf("GetDefaultScope() = %q, want %q", got, "alice")
	}

	if err := ctx.SetDefaultScope("ALICE"); err == nil {
		t.Fatal("SetDefaultScope() succeeded unexpectedly")
	}
}

func Test_profileFileName(t *testing.T) {
	got := profileFileName(testFullEnv("alice", "dev"))
	if got != "alice__dev.json" {
		t.Fatalf("profileFileName() = %q, want %q", got, "alice__dev.json")
	}
}

func TestDeployEnv_Active(t *testing.T) {
	newBazelWorkspace(t)
	internalInit()

	env := &DeployEnv{Profile: Profile{Name: testFullEnv("alice", "dev")}}
	if err := env.Active(); err != nil {
		t.Fatalf("Active() failed: %v", err)
	}

	ctxInfo, err := LoadDeployContext()
	if err != nil {
		t.Fatalf("loadDeployContext() failed: %v", err)
	}
	want := &DeployContext{
		ActiveEnv:    testFullEnv("alice", "dev"),
		DefaultScope: "alice",
	}
	if !reflect.DeepEqual(ctxInfo, want) {
		t.Fatalf("loadDeployContext() = %v, want %v", ctxInfo, want)
	}
}

func TestCurrent_NotActive(t *testing.T) {
	newBazelWorkspace(t)
	internalInit()

	_, err := Current()
	if !errors.Is(err, ErrNotActive) {
		t.Fatalf("Current() err = %v, want %v", err, ErrNotActive)
	}
}

func TestDeployEnv_Delete_RequiresExecutor(t *testing.T) {
	env := &DeployEnv{Profile: Profile{Name: testFullEnv("alice", "dev")}}
	if err := env.Delete(context.Background(), nil); err == nil {
		t.Fatal("Delete() succeeded unexpectedly")
	}
}

func TestDeployEnv_Delete_ClearsActiveContext(t *testing.T) {
	root := newBazelWorkspace(t)
	internalInit()

	fullEnv := testFullEnv("alice", "dev")
	env := &DeployEnv{Profile: Profile{Name: fullEnv}}

	if err := (&DeployContext{ActiveEnv: fullEnv, DefaultScope: "alice"}).Save(); err != nil {
		t.Fatalf("saveDeployContext() failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, profileDir, profileFileName(fullEnv)), []byte(`{"env_name":"alice.dev"}`), os.ModePerm); err != nil {
		t.Fatalf("WriteFile(profile) failed: %v", err)
	}

	deletedEnvironment := ""
	err := env.Delete(context.Background(), &stubExecutor{deleteFunc: func(_ context.Context, environment string) error {
		deletedEnvironment = environment
		return nil
	}})
	if err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}
	if deletedEnvironment != string(fullEnv) {
		t.Fatalf("Delete() environment = %q, want %q", deletedEnvironment, string(fullEnv))
	}

	ctxInfo, err := LoadDeployContext()
	if err != nil {
		t.Fatalf("loadDeployContext() failed: %v", err)
	}
	if ctxInfo.ActiveEnv != EmpytEnvName {
		t.Fatalf("ActiveEnv = %q, want empty", ctxInfo.ActiveEnv)
	}
	if ctxInfo.DefaultScope != "alice" {
		t.Fatalf("DefaultScope = %q, want %q", ctxInfo.DefaultScope, "alice")
	}
	if _, err := os.Stat(filepath.Join(root, profileDir, profileFileName(fullEnv))); !os.IsNotExist(err) {
		t.Fatalf("profile still exists, stat err = %v", err)
	}
}

func TestDeployEnv_Update(t *testing.T) {
	root := newBazelWorkspace(t)
	internalInit()
	withNow(t, time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC))

	fullEnv := testFullEnv("alice", "dev")
	env := &DeployEnv{Profile: Profile{Name: fullEnv}}
	deployConfig := &config.DeployConfig{
		Name: "alice.dev",
		Desc: "开发环境",
		URI:  "//deploy/grpc-hello-world/deploy.yaml",
		Services: []*config.DeployService{
			{Artifact: config.DeployArtifact{Path: "//service/service.yaml", Name: "service"}},
			{Artifact: config.DeployArtifact{Path: "//gateway/service.yaml", Name: "gateway"}},
		},
	}

	if err := env.Update(deployConfig); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	if env.RemoteStatus != RemoteStatusPending {
		t.Fatalf("RemoteStatus = %q, want %q", env.RemoteStatus, RemoteStatusPending)
	}
	if env.deployConfig != deployConfig {
		t.Fatal("deployConfig not cached on env")
	}
	if len(env.serviceConfigs) != 2 {
		t.Fatalf("serviceConfigs len = %d, want 2", len(env.serviceConfigs))
	}

	for _, filePath := range []string{
		filepath.Join(root, profileDir, profileFileName(fullEnv)),
		filepath.Join(root, deployConfigDir, env.deployConfigFileName()),
		filepath.Join(root, serviceConfigDir, env.serviceConfigFileName()),
	} {
		if _, err := os.Stat(filePath); err != nil {
			t.Fatalf("expected file %q to exist: %v", filePath, err)
		}
	}

	loaded, err := loadDeployEnv(filepath.Join(root, profileDir, profileFileName(fullEnv)))
	if err != nil {
		t.Fatalf("loadDeployEnv() failed: %v", err)
	}
	if loaded.Name != fullEnv {
		t.Fatalf("loaded.Name = %q, want %q", loaded.Name, fullEnv)
	}
	if loaded.RemoteStatus != RemoteStatusPending {
		t.Fatalf("loaded.RemoteStatus = %q, want %q", loaded.RemoteStatus, RemoteStatusPending)
	}
	if loaded.deployConfig == nil {
		t.Fatal("loaded deployConfig is nil")
	}
	if len(loaded.serviceConfigs) != 2 {
		t.Fatalf("loaded serviceConfigs len = %d, want 2", len(loaded.serviceConfigs))
	}
}

func TestDeployEnv_Deploy_RequiresExecutor(t *testing.T) {
	env := &DeployEnv{Profile: Profile{Name: testFullEnv("alice", "dev")}}
	if err := env.Deploy(context.Background(), nil); err == nil {
		t.Fatal("Deploy() succeeded unexpectedly")
	}
}

func TestDeployEnv_Deploy_RequiresCachedConfig(t *testing.T) {
	env := &DeployEnv{Profile: Profile{Name: testFullEnv("alice", "dev")}}
	err := env.Deploy(context.Background(), &stubExecutor{})
	if err == nil || !strings.Contains(err.Error(), "call Update first") {
		t.Fatalf("Deploy() err = %v, want missing cached config error", err)
	}
}

func TestDeployEnv_Deploy_SuccessUpdatesStatus(t *testing.T) {
	newBazelWorkspace(t)
	internalInit()
	withNow(t, time.Date(2026, 4, 12, 1, 0, 0, 0, time.UTC))

	fullEnv := testFullEnv("alice", "dev")
	env := &DeployEnv{Profile: Profile{Name: fullEnv}}
	deployConfig := &config.DeployConfig{
		Name:     "alice.dev",
		Desc:     "开发环境",
		URI:      "//deploy/grpc-hello-world/deploy.yaml",
		Services: []*config.DeployService{{Artifact: config.DeployArtifact{Path: "//service/service.yaml", Name: "service"}}},
	}
	if err := env.Update(deployConfig); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	withImageResolver(t, &stubImageResolver{resolveFunc: func(context.Context, *config.DeployConfig, []*config.ServiceConfig) (map[string]string, error) {
		return map[string]string{"//service:service_image": "registry.example.com/team/service@sha256:111"}, nil
	}})

	var gotObjects *k8s.DeployObjects
	applyCalled := false
	err := env.Deploy(context.Background(), &stubExecutor{applyFunc: func(_ context.Context, objects *k8s.DeployObjects) error {
		applyCalled = true
		gotObjects = objects
		if objects == nil {
			t.Fatal("Apply() objects = nil, want deploy objects")
		}
		if len(objects.Deployments) != 1 {
			t.Fatalf("Apply() deployments len = %d, want 1", len(objects.Deployments))
		}
		if len(objects.HTTPRoutes) != 0 {
			t.Fatalf("Apply() routes len = %d, want 0", len(objects.HTTPRoutes))
		}
		if got := objects.Deployments[0].Image; got != "registry.example.com/team/service@sha256:111" {
			t.Fatalf("Apply() deployment image = %q, want resolved image", got)
		}
		if got := objects.Deployments[0].ServiceName; got != "service" {
			t.Fatalf("Apply() deployment service = %q, want %q", got, "service")
		}
		if got := objects.Deployments[0].EnvironmentName; got != string(fullEnv) {
			t.Fatalf("Apply() deployment env = %q, want %q", got, string(fullEnv))
		}
		return nil
	}})
	if err != nil {
		t.Fatalf("Deploy() failed: %v", err)
	}
	if !applyCalled {
		t.Fatal("Deploy() did not call Apply()")
	}
	if gotObjects == nil {
		t.Fatal("Apply() objects not captured")
	}
	if env.RemoteStatus != RemoteStatusDeployed {
		t.Fatalf("RemoteStatus = %q, want %q", env.RemoteStatus, RemoteStatusDeployed)
	}
}

func TestCollectReferencedArtifactTargets(t *testing.T) {
	deployConfig := &config.DeployConfig{
		Name: "alice.dev",
		Services: []*config.DeployService{
			{Artifact: config.DeployArtifact{Path: "//service/service.yaml", Name: "service"}},
			{Artifact: config.DeployArtifact{Path: "//gateway/service.yaml", Name: "gateway"}},
			{Infra: config.DeployInfra{Resource: "mongodb", Profile: "dev", Name: "mongo", App: "grpc-hello-world"}},
		},
	}
	serviceConfigs := []*config.ServiceConfig{
		{
			URI:       "//service/service.yaml",
			Name:      "service",
			App:       "grpc-hello-world",
			Artifacts: []*config.ServiceArtifact{{Name: "service", Type: config.ServiceArtifactTypeDeployment, Target: "//service:service_image"}},
		},
		{
			URI:       "//gateway/service.yaml",
			Name:      "gateway",
			App:       "grpc-hello-world",
			Artifacts: []*config.ServiceArtifact{{Name: "gateway", Type: config.ServiceArtifactTypeDeployment, Target: "//gateway:gateway_image"}},
		},
	}

	got, err := collectReferencedArtifactTargets(deployConfig, serviceConfigs)
	if err != nil {
		t.Fatalf("collectReferencedArtifactTargets() failed: %v", err)
	}
	want := []string{"//gateway:gateway_image", "//service:service_image"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectReferencedArtifactTargets() = %v, want %v", got, want)
	}
}

func TestBuildDeployObjects_ReturnsDeployObjects(t *testing.T) {
	env := &DeployEnv{
		Profile: Profile{Name: testFullEnv("alice", "dev")},
		deployConfig: &config.DeployConfig{
			Services: []*config.DeployService{{Artifact: config.DeployArtifact{Path: "//service/service.yaml", Name: "service"}}},
		},
		serviceConfigs: []*config.ServiceConfig{{
			URI:  "//service/service.yaml",
			Name: "service",
			App:  "grpc-hello-world",
			Desc: "service config",
			Artifacts: []*config.ServiceArtifact{{
				Name:   "service",
				Type:   config.ServiceArtifactTypeDeployment,
				Target: "//service:service_image",
			}},
		}},
	}

	objects, err := env.BuildDeployObjects(map[string]string{"//service:service_image": "registry.example.com/team/service@sha256:111"})
	if err != nil {
		t.Fatalf("BuildDeployObjects() failed: %v", err)
	}
	if objects == nil {
		t.Fatal("BuildDeployObjects() returned nil objects")
	}
	if len(objects.Deployments) != 1 {
		t.Fatalf("BuildDeployObjects() deployments len = %d, want 1", len(objects.Deployments))
	}
	if len(objects.HTTPRoutes) != 0 {
		t.Fatalf("BuildDeployObjects() routes len = %d, want 0", len(objects.HTTPRoutes))
	}
	if got := objects.Deployments[0].Image; got != "registry.example.com/team/service@sha256:111" {
		t.Fatalf("BuildDeployObjects() deployment image = %q, want resolved image", got)
	}
}
