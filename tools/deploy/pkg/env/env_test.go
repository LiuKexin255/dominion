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
	"dominion/tools/deploy/pkg/imagepush"
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

func newBazelWorkspace(t *testing.T, envDir string) string {
	t.Helper()

	srcRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	copyDir(t, filepath.Join(srcRoot, "testdata"), filepath.Join(root, "testdata"))
	copyDir(t, filepath.Join(root, "testdata", "service"), filepath.Join(root, "service"))
	copyDir(t, filepath.Join(root, "testdata", "gateway"), filepath.Join(root, "gateway"))
	if envDir != "" {
		copyDir(t, filepath.Join(root, "testdata", envDir), filepath.Join(root, ".env"))
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

// stubExecutor 为 env 层测试提供可注入的执行器替身。
type stubExecutor struct {
	applyFunc  func(ctx context.Context, objects *k8s.DeployObjects) error
	deleteFunc func(ctx context.Context, app, environment string) error
}

func (e *stubExecutor) Apply(ctx context.Context, objects *k8s.DeployObjects) error {
	if e == nil || e.applyFunc == nil {
		return nil
	}
	return e.applyFunc(ctx, objects)
}

func (e *stubExecutor) Delete(ctx context.Context, app, environment string) error {
	if e == nil || e.deleteFunc == nil {
		return nil
	}
	return e.deleteFunc(ctx, app, environment)
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

type stubImagePushRunner struct {
	runFunc func(ctx context.Context, pushTarget string) (*imagepush.PushOutput, error)
}

func (r *stubImagePushRunner) Run(ctx context.Context, pushTarget string) (*imagepush.PushOutput, error) {
	if r == nil || r.runFunc == nil {
		return nil, nil
	}
	return r.runFunc(ctx, pushTarget)
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
	dir := newBazelWorkspace(t, "")
	internalInit()

	want := &DeployContext{
		ActiveEnv: &EnvRef{Name: "test-Name", App: "app-test"},
		LastApp:   "app-test",
	}

	if err := saveDeployContext(want); err != nil {
		t.Fatalf("saveDeployContext() failed: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, currentEnvPath))
	if err != nil {
		t.Fatalf("os.ReadFile() failed: %v", err)
	}
	if got := string(raw); !strings.Contains(got, `"active_env"`) || !strings.Contains(got, `"last_app":"app-test"`) {
		t.Fatalf("saved json = %s, want deploy context fields", got)
	}

	got, err := loadDeployContext()
	if err != nil {
		t.Fatalf("loadDeployContext() failed: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("loadDeployContext() = %v, want %v", got, want)
	}
}

func TestDeployContext_LoadMissingFile(t *testing.T) {
	newBazelWorkspace(t, "")
	internalInit()

	got, err := loadDeployContext()
	if err != nil {
		t.Fatalf("loadDeployContext() failed: %v", err)
	}

	if !reflect.DeepEqual(got, &DeployContext{}) {
		t.Fatalf("loadDeployContext() = %v, want empty context", got)
	}
}

func TestDeployContext_LoadCorruptJSON(t *testing.T) {
	dir := newBazelWorkspace(t, "")
	internalInit()

	if err := os.WriteFile(filepath.Join(dir, currentEnvPath), []byte("{broken json"), os.ModePerm); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}

	_, err := loadDeployContext()
	if err == nil {
		t.Fatal("loadDeployContext() succeeded unexpectedly")
	}
	if !strings.Contains(err.Error(), "load deploy context") {
		t.Fatalf("loadDeployContext() error = %v, want context error", err)
	}
}

func TestDefaultApp_WithLastAppOnly(t *testing.T) {
	newBazelWorkspace(t, "")
	internalInit()

	if err := saveDeployContext(&DeployContext{LastApp: "cached-app"}); err != nil {
		t.Fatalf("saveDeployContext() failed: %v", err)
	}

	got, err := DefaultApp()
	if err != nil {
		t.Fatalf("DefaultApp() failed: %v", err)
	}
	if got != "cached-app" {
		t.Fatalf("DefaultApp() = %q, want %q", got, "cached-app")
	}
}

func Test_profileName(t *testing.T) {
	tests := []struct {
		testName string // description of this test case
		// Named input parameters for target function.
		name string
		app  string
		want string
	}{
		{
			testName: "profile name",
			name:     "ttttest",
			app:      "aapp",
			want:     "aapp__ttttest.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := profileName(tt.name, tt.app)
			if tt.want != got {
				t.Errorf("profileName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		caseName string // description of this test case
		name     string
		app      string
		envDir   string
		want     *DeployEnv
		wantErr  bool
	}{
		{
			caseName: "创建新的环境",
			name:     "test_env",
			app:      "test_app",
			envDir:   "",
			want: &DeployEnv{
				Profile: Profile{
					Name: "test_env",
					App:  "test_app",
				},
			},
		},
		{
			caseName: "环境已存在",
			envDir:   ".env",
			name:     "test_env",
			app:      "grpc-hello-world",
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.caseName, func(t *testing.T) {
			nowTime := time.Now().UTC().Round(0)
			now = func() time.Time {
				return nowTime
			}

			newBazelWorkspace(t, tt.envDir)

			internalInit()

			got, gotErr := New(tt.name, tt.app)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("New() failed: %v", gotErr)
				}
				return
			}

			if tt.wantErr {
				t.Fatal("New() succeeded unexpectedly")
			}

			tt.want.UpdatedAt = nowTime
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("New() = %v, want %v", got, tt.want)
			}

			// 读取配置
			env, gotErr := Get(tt.name, tt.app)
			if gotErr != nil {
				t.Fatalf("Get() failed: %v", gotErr)
			}
			if !reflect.DeepEqual(env, tt.want) {
				t.Errorf("Get() = %v, want %v", env, tt.want)
			}
		})
	}
}

func TestGet(t *testing.T) {
	updatedAt, err := time.Parse(time.RFC3339Nano, "2026-03-24T07:38:55.742159784Z")
	if err != nil {
		t.Fatalf("time.Parse() failed: %v", err)
	}

	tests := []struct {
		caseName string // description of this test case
		// Named input parameters for target function.
		name    string
		app     string
		want    *DeployEnv
		wantErr bool
	}{
		{
			caseName: "环境不存在",
			name:     "ttttttt",
			app:      "grpc-hello-world",
			wantErr:  true,
		},
		{
			caseName: "主配置文件读取失败",
			name:     "test_env",
			app:      "grpc-hello",
			wantErr:  true,
		},
		{
			caseName: "正常读取",
			name:     "test_env",
			app:      "grpc-hello-world",
			want: &DeployEnv{
				Profile: Profile{
					Name:       "test_env",
					App:        "grpc-hello-world",
					UpdatedAt:  updatedAt,
					MainConfig: "grpc-hello-world__test_env__grpc-hello-world__deploy.yaml",
				},
				mainDeployConfig: &config.DeployConfig{
					Template: "deploy",
					App:      "grpc-hello-world",
					Desc:     "开发环境",
					URI:      "//deploy/grpc-hello-world/deploy.yaml",
					Services: []*config.DeployService{
						{
							Artifact: config.DeployArtifact{
								Path: "//service/service.yaml",
								Name: "service",
							},
						},
						{
							Artifact: config.DeployArtifact{
								Path: "//gateway/service.yaml",
								Name: "gateway",
							},
							HTTP: config.DeployHTTP{
								Hostnames: []string{"hello.liukexin.com"},
								Matches: []*config.DeployHTTPMatch{
									{
										Backend: "http",
										Path: config.DeployHTTPPathMatch{
											Type:  config.HTTPPathMatchTypePrefix,
											Value: "/v1",
										},
									},
								},
							},
						},
					},
				},
				serviceConfigs: []*config.ServiceConfig{
					{
						Name: "service",
						App:  "grpc-hello-world",
						Desc: "service config",
						URI:  "//service/service.yaml",
						Artifacts: []*config.ServiceArtifact{
							{
								Name:   "service",
								Type:   config.ServiceArtifactTypeDeployment,
								Target: "//service:service_image",
								Ports: []*config.ServiceArtifactPort{
									{
										Name: "grpc",
										Port: 50051,
									},
								},
							},
							{
								Name:   "service111",
								Type:   config.ServiceArtifactTypeDeployment,
								Target: "//service:service111_image",
								Ports: []*config.ServiceArtifactPort{
									{
										Name: "grpc",
										Port: 50052,
									},
								},
							},
						},
					},
					{
						Name: "gateway",
						App:  "grpc-hello-world",
						Desc: "gateway config",
						URI:  "//gateway/service.yaml",
						Artifacts: []*config.ServiceArtifact{
							{
								Name:   "gateway",
								Type:   config.ServiceArtifactTypeDeployment,
								Target: "//gateway:gateway_image",
								Ports: []*config.ServiceArtifactPort{
									{
										Name: "http",
										Port: 80,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			caseName: "读取空环境",
			name:     "empty",
			app:      "env",
			want: &DeployEnv{
				Profile: Profile{
					Name:      "empty",
					App:       "env",
					UpdatedAt: updatedAt,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.caseName, func(t *testing.T) {
			newBazelWorkspace(t, ".env")
			internalInit()

			got, gotErr := Get(tt.name, tt.app)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("Get() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("Get() succeeded unexpectedly")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Get() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestList(t *testing.T) {
	updatedAt, err := time.Parse(time.RFC3339Nano, "2026-03-24T07:38:55.742159784Z")
	if err != nil {
		t.Fatalf("time.Parse() failed: %v", err)
	}

	tests := []struct {
		name      string
		copyLists bool
		want      []*DeployEnv
	}{
		{
			name:      "读取所有环境",
			copyLists: true,
			want: []*DeployEnv{
				{
					Profile: Profile{
						Name:      "empty",
						App:       "env",
						UpdatedAt: updatedAt,
					},
				},
				{
					Profile: Profile{
						Name:       "test_env",
						App:        "grpc-hello-world",
						UpdatedAt:  updatedAt,
						MainConfig: "grpc-hello-world__test_env__grpc-hello-world__deploy.yaml",
					},
					mainDeployConfig: &config.DeployConfig{
						Template: "deploy",
						App:      "grpc-hello-world",
						Desc:     "开发环境",
						URI:      "//deploy/grpc-hello-world/deploy.yaml",
						Services: []*config.DeployService{
							{
								Artifact: config.DeployArtifact{
									Path: "//service/service.yaml",
									Name: "service",
								},
							},
							{
								Artifact: config.DeployArtifact{
									Path: "//gateway/service.yaml",
									Name: "gateway",
								},
								HTTP: config.DeployHTTP{
									Hostnames: []string{"hello.liukexin.com"},
									Matches: []*config.DeployHTTPMatch{
										{
											Backend: "http",
											Path: config.DeployHTTPPathMatch{
												Type:  config.HTTPPathMatchTypePrefix,
												Value: "/v1",
											},
										},
									},
								},
							},
						},
					},
					serviceConfigs: []*config.ServiceConfig{
						{
							Name: "service",
							App:  "grpc-hello-world",
							Desc: "service config",
							URI:  "//service/service.yaml",
							Artifacts: []*config.ServiceArtifact{
								{
									Name:   "service",
									Type:   config.ServiceArtifactTypeDeployment,
									Target: "//service:service_image",
									Ports: []*config.ServiceArtifactPort{
										{
											Name: "grpc",
											Port: 50051,
										},
									},
								},
								{
									Name:   "service111",
									Type:   config.ServiceArtifactTypeDeployment,
									Target: "//service:service111_image",
									Ports: []*config.ServiceArtifactPort{
										{
											Name: "grpc",
											Port: 50052,
										},
									},
								},
							},
						},
						{
							Name: "gateway",
							App:  "grpc-hello-world",
							Desc: "gateway config",
							URI:  "//gateway/service.yaml",
							Artifacts: []*config.ServiceArtifact{
								{
									Name:   "gateway",
									Type:   config.ServiceArtifactTypeDeployment,
									Target: "//gateway:gateway_image",
									Ports: []*config.ServiceArtifactPort{
										{
											Name: "http",
											Port: 80,
										},
									},
								},
							},
						},
					},
				},
				{
					Profile: Profile{
						Name:       "test_env_v2",
						App:        "grpc-hello-world",
						UpdatedAt:  updatedAt,
						MainConfig: "grpc-hello-world__test_env_v2__grpc-hello-world__deploy.yaml",
					},
					mainDeployConfig: &config.DeployConfig{
						Template: "deploy",
						App:      "grpc-hello-world",
						Desc:     "开发环境1111",
						URI:      "//deploy/grpc-hello-world/deploy_v2.yaml",
						Services: []*config.DeployService{
							{
								Artifact: config.DeployArtifact{
									Path: "//service/service.yaml",
									Name: "service1111",
								},
							},
							{
								Artifact: config.DeployArtifact{
									Path: "//gateway/service.yaml",
									Name: "gateway",
								},
								HTTP: config.DeployHTTP{
									Hostnames: []string{"hello.liukexin.com2222"},
									Matches: []*config.DeployHTTPMatch{
										{
											Backend: "http",
											Path: config.DeployHTTPPathMatch{
												Type:  config.HTTPPathMatchTypePrefix,
												Value: "/v1",
											},
										},
									},
								},
							},
						},
					},
					serviceConfigs: []*config.ServiceConfig{
						{
							Name: "service",
							App:  "grpc-hello-world",
							Desc: "service config",
							URI:  "//service/service.yaml",
							Artifacts: []*config.ServiceArtifact{
								{
									Name:   "service",
									Type:   config.ServiceArtifactTypeDeployment,
									Target: "//service:service_image",
									Ports: []*config.ServiceArtifactPort{
										{
											Name: "grpc",
											Port: 50051,
										},
									},
								},
								{
									Name:   "service111",
									Type:   config.ServiceArtifactTypeDeployment,
									Target: "//service:service111_image",
									Ports: []*config.ServiceArtifactPort{
										{
											Name: "grpc",
											Port: 50052,
										},
									},
								},
							},
						},
						{
							Name: "gateway",
							App:  "grpc-hello-world",
							Desc: "gateway config",
							URI:  "//gateway/service.yaml",
							Artifacts: []*config.ServiceArtifact{
								{
									Name:   "gateway",
									Type:   config.ServiceArtifactTypeDeployment,
									Target: "//gateway:gateway_image",
									Ports: []*config.ServiceArtifactPort{
										{
											Name: "http",
											Port: 80,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "无环境返回空切片",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envDir := ""
			if tt.copyLists {
				envDir = filepath.Join("list", ".env")
			}
			newBazelWorkspace(t, envDir)
			internalInit()

			got, err := List()
			if err != nil {
				t.Fatalf("List() failed: %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("List() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeployEnv_Active(t *testing.T) {
	tests := []struct {
		caseName string // description of this test case
		name1    string
		name2    string
		app      string
	}{
		{
			caseName: "当前环境切换",
			name2:    "test_env_v2",
			name1:    "test_env",
			app:      "grpc-hello-world",
		},
	}
	for _, tt := range tests {
		t.Run(tt.caseName, func(t *testing.T) {
			newBazelWorkspace(t, ".env")
			internalInit()

			_, gotErr := Current()
			if gotErr == nil || !errors.Is(gotErr, ErrNotActive) {
				t.Fatalf("Current gotErr = %v, want %v", gotErr, ErrNotActive)
			}

			env, gotErr := Get(tt.name1, tt.app)
			if gotErr != nil {
				t.Fatalf("Get() failed: %v", gotErr)
			}

			gotErr = env.Active()
			if gotErr != nil {
				t.Fatalf("Get() failed: %v", gotErr)
			}

			ctxInfo, gotErr := loadDeployContext()
			if gotErr != nil {
				t.Fatalf("loadDeployContext() failed: %v", gotErr)
			}
			if !reflect.DeepEqual(ctxInfo, &DeployContext{ActiveEnv: &EnvRef{Name: tt.name1, App: tt.app}, LastApp: tt.app}) {
				t.Fatalf("loadDeployContext() = %v, want active_env=%s/%s last_app=%s", ctxInfo, tt.app, tt.name1, tt.app)
			}

			curEnv, gotErr := Current()
			if gotErr != nil {
				t.Fatalf("Current() failed: %v", gotErr)
			}

			if !reflect.DeepEqual(env, curEnv) {
				t.Fatalf("Current() = %v, want %v", env, curEnv)
			}

			// 切换环境

			env, gotErr = Get(tt.name2, tt.app)
			if gotErr != nil {
				t.Fatalf("Get() failed: %v", gotErr)
			}

			gotErr = env.Active()
			if gotErr != nil {
				t.Fatalf("Get() failed: %v", gotErr)
			}

			ctxInfo, gotErr = loadDeployContext()
			if gotErr != nil {
				t.Fatalf("loadDeployContext() failed: %v", gotErr)
			}
			if !reflect.DeepEqual(ctxInfo, &DeployContext{ActiveEnv: &EnvRef{Name: tt.name2, App: tt.app}, LastApp: tt.app}) {
				t.Fatalf("loadDeployContext() = %v, want active_env=%s/%s last_app=%s", ctxInfo, tt.app, tt.name2, tt.app)
			}

			curEnv, gotErr = Current()
			if gotErr != nil {
				t.Fatalf("Current() failed: %v", gotErr)
			}

			if !reflect.DeepEqual(env, curEnv) {
				t.Fatalf("Current() = %v, want %v", env, curEnv)
			}

			gotErr = curEnv.Delete(context.Background(), &stubExecutor{})
			if gotErr != nil {
				t.Fatalf("Delete() failed: %v", gotErr)
			}

			curEnv, gotErr = Current()
			if gotErr == nil || !errors.Is(gotErr, ErrNotActive) {
				t.Fatalf("Current() after Delete() gotErr = %v, want %v", gotErr, ErrNotActive)
			}

			ctxInfo, gotErr = loadDeployContext()
			if gotErr != nil {
				t.Fatalf("loadDeployContext() failed: %v", gotErr)
			}
			if ctxInfo.ActiveEnv != nil {
				t.Fatalf("loadDeployContext() active_env = %v, want nil", ctxInfo.ActiveEnv)
			}
			if ctxInfo.LastApp != tt.app {
				t.Fatalf("loadDeployContext() last_app = %q, want %q", ctxInfo.LastApp, tt.app)
			}
		})
	}
}

func TestDeployEnv_Delete(t *testing.T) {
	tests := []struct {
		caseName string // description of this test case
		name     string
		app      string
	}{
		{
			caseName: "正常删除",
			name:     "test_env",
			app:      "grpc-hello-world",
		},
		{
			caseName: "删除新建环境",
			name:     "empty",
			app:      "env",
		},
	}
	for _, tt := range tests {
		t.Run(tt.caseName, func(t *testing.T) {
			newBazelWorkspace(t, ".env")
			internalInit()

			e, gotErr := Get(tt.name, tt.app)
			if gotErr != nil {
				t.Fatalf("Get() failed: %v", gotErr)
			}

			gotErr = e.Delete(context.Background(), &stubExecutor{})
			if gotErr != nil {
				t.Fatalf("Delete() failed: %v", gotErr)
			}

			_, gotErr = Get(tt.name, tt.app)
			if gotErr == nil || !errors.Is(gotErr, ErrNotFound) {
				t.Fatalf("Get() after Delete() gotErr = %v, want %v", gotErr, ErrNotFound)
			}
		})
	}
}

func TestDeployEnv_Delete_NonCurrentPreservesContext(t *testing.T) {
	newBazelWorkspace(t, ".env")
	internalInit()

	activeEnv, err := Get("test_env_v2", "grpc-hello-world")
	if err != nil {
		t.Fatalf("Get(active) failed: %v", err)
	}
	if err := activeEnv.Active(); err != nil {
		t.Fatalf("Active() failed: %v", err)
	}

	ctxBefore, err := loadDeployContext()
	if err != nil {
		t.Fatalf("loadDeployContext() before delete failed: %v", err)
	}

	deleteEnv, err := Get("test_env", "grpc-hello-world")
	if err != nil {
		t.Fatalf("Get(delete target) failed: %v", err)
	}
	if err := deleteEnv.Delete(context.Background(), &stubExecutor{}); err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}

	curEnv, err := Current()
	if err != nil {
		t.Fatalf("Current() after deleting non-current env failed: %v", err)
	}
	if !curEnv.Equal(activeEnv) {
		t.Fatalf("Current() = %v, want %v", curEnv, activeEnv)
	}

	ctxAfter, err := loadDeployContext()
	if err != nil {
		t.Fatalf("loadDeployContext() after delete failed: %v", err)
	}
	if !reflect.DeepEqual(ctxAfter, ctxBefore) {
		t.Fatalf("context changed after deleting non-current env: got %v, want %v", ctxAfter, ctxBefore)
	}
}

func TestCurrent_ActiveEnvProfileMissing(t *testing.T) {
	newBazelWorkspace(t, "")
	internalInit()

	if err := saveDeployContext(&DeployContext{ActiveEnv: &EnvRef{Name: "missing", App: "missing-app"}, LastApp: "cached-app"}); err != nil {
		t.Fatalf("saveDeployContext() failed: %v", err)
	}

	_, err := Current()
	if err == nil {
		t.Fatal("Current() succeeded unexpectedly")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Current() err = %v, want %v", err, ErrNotFound)
	}
}

func TestDeployEnv_Deploy_RequiresExecutor(t *testing.T) {
	for _, tt := range []struct {
		caseName string
		name     string
		app      string
		exec     executor
		wantErr  bool
	}{
		{caseName: "nil executor", name: "test_env", app: "grpc-hello-world", wantErr: true},
		{caseName: "valid executor", name: "test_env", app: "grpc-hello-world", exec: &stubExecutor{}, wantErr: false},
	} {
		t.Run(tt.caseName, func(t *testing.T) {
			newBazelWorkspace(t, ".env")
			internalInit()

			env, err := Get(tt.name, tt.app)
			if err != nil {
				t.Fatalf("Get() failed: %v", err)
			}
			if tt.exec != nil {
				withImageResolver(t, &stubImageResolver{
					resolveFunc: func(context.Context, *config.DeployConfig, []*config.ServiceConfig) (map[string]string, error) {
						return map[string]string{
							"//service:service_image": "registry.example.com/team/service@sha256:1111111111111111111111111111111111111111111111111111111111111111",
							"//gateway:gateway_image": "registry.example.com/team/gateway@sha256:2222222222222222222222222222222222222222222222222222222222222222",
						}, nil
					},
				})
			}

			if err := env.Update(&config.DeployConfig{
				Template: "deploy",
				App:      "grpc-hello-world",
				Desc:     "开发环境",
				URI:      "//deploy/grpc-hello-world/deploy.yaml",
				Services: []*config.DeployService{
					{Artifact: config.DeployArtifact{Path: "//service/service.yaml", Name: "service"}},
					{Artifact: config.DeployArtifact{Path: "//gateway/service.yaml", Name: "gateway"}},
				},
			}); err != nil {
				t.Fatalf("Update() failed: %v", err)
			}

			err = env.Deploy(context.Background(), tt.exec)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Deploy() succeeded unexpectedly")
				}
			} else if err != nil {
				t.Fatalf("Deploy() failed: %v", err)
			}

			cached, err := Get(tt.name, tt.app)
			if err != nil {
				t.Fatalf("Get() after Deploy() failed: %v", err)
			}
			wantStatus := RemoteStatusPending
			if !tt.wantErr {
				wantStatus = RemoteStatusDeployed
			}
			if cached.RemoteStatus != wantStatus {
				t.Fatalf("Deploy() remote_status = %q, want %q", cached.RemoteStatus, wantStatus)
			}
		})
	}
}

func TestDeployEnv_Delete_RequiresExecutor(t *testing.T) {
	for _, tt := range []struct {
		caseName string
		name     string
		app      string
		exec     executor
		wantErr  bool
	}{
		{caseName: "nil executor", name: "test_env", app: "grpc-hello-world", wantErr: true},
		{caseName: "valid executor", name: "test_env", app: "grpc-hello-world", exec: &stubExecutor{}},
	} {
		t.Run(tt.caseName, func(t *testing.T) {
			newBazelWorkspace(t, ".env")
			internalInit()

			env, err := Get(tt.name, tt.app)
			if err != nil {
				t.Fatalf("Get() failed: %v", err)
			}

			err = env.Delete(context.Background(), tt.exec)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Delete() succeeded unexpectedly")
				}
			} else if err != nil {
				t.Fatalf("Delete() failed: %v", err)
			}

			cached, err := Get(tt.name, tt.app)
			if tt.wantErr {
				if err != nil {
					t.Fatalf("Get() after Delete() failed: %v", err)
				}
				if !cached.Equal(env) {
					t.Fatalf("Get() after failed Delete() = %v, want %v", cached, env)
				}
			} else {
				if err == nil || !errors.Is(err, ErrNotFound) {
					t.Fatalf("Get() after Delete() err = %v, want %v", err, ErrNotFound)
				}
			}
		})
	}
}

func TestDeployEnv_Deploy_FailedRemainsPending(t *testing.T) {
	dir := newBazelWorkspace(t, ".env")

	internalInit()

	env, err := Get("empty", "env")
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	deployConfig := &config.DeployConfig{
		Template: "deploy",
		App:      "grpc-hello-world",
		Desc:     "开发环境",
		URI:      "//deploy/grpc-hello-world/deploy.yaml",
		Services: []*config.DeployService{
			{Artifact: config.DeployArtifact{Path: "//service/service.yaml", Name: "service"}},
			{Artifact: config.DeployArtifact{Path: "//gateway/service.yaml", Name: "gateway"}},
		},
	}
	if err := env.Update(deployConfig); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}
	withImageResolver(t, &stubImageResolver{
		resolveFunc: func(context.Context, *config.DeployConfig, []*config.ServiceConfig) (map[string]string, error) {
			return map[string]string{
				"//service:service_image": "registry.example.com/team/service@sha256:1111111111111111111111111111111111111111111111111111111111111111",
				"//gateway:gateway_image": "registry.example.com/team/gateway@sha256:2222222222222222222222222222222222222222222222222222222222222222",
			}, nil
		},
	})

	wantErr := errors.New("apply failed")
	exec := &stubExecutor{applyFunc: func(context.Context, *k8s.DeployObjects) error { return wantErr }}
	if err := env.Deploy(context.Background(), exec); !errors.Is(err, wantErr) {
		t.Fatalf("Deploy() err = %v, want %v", err, wantErr)
	}

	cached, err := Get("empty", "env")
	if err != nil {
		t.Fatalf("Get() after Deploy() failed: %v", err)
	}
	if cached.RemoteStatus != RemoteStatusPending {
		t.Fatalf("Deploy() remote_status = %q, want %q", cached.RemoteStatus, RemoteStatusPending)
	}
	if cached.MainConfig == "" {
		t.Fatal("Deploy() removed cached deploy config unexpectedly")
	}
	if _, err := os.Stat(filepath.Join(dir, ".env", "deploy", cached.MainConfig)); err != nil {
		t.Fatalf("deploy cache missing after failed Deploy(): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env", "service", "env__empty.yaml")); err != nil {
		t.Fatalf("service cache missing after failed Deploy(): %v", err)
	}
}

func TestDeploy_DoesNotPushDuringUpdate(t *testing.T) {
	newBazelWorkspace(t, ".env")
	internalInit()

	env, err := Get("empty", "env")
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	pushCalls := 0
	withImageResolver(t, &stubImageResolver{
		resolveFunc: func(context.Context, *config.DeployConfig, []*config.ServiceConfig) (map[string]string, error) {
			pushCalls++
			return map[string]string{}, nil
		},
	})

	deployConfig := &config.DeployConfig{
		Template: "deploy",
		App:      "grpc-hello-world",
		Desc:     "开发环境",
		URI:      "//deploy/grpc-hello-world/deploy.yaml",
		Services: []*config.DeployService{
			{Artifact: config.DeployArtifact{Path: "//service/service.yaml", Name: "service"}},
			{Artifact: config.DeployArtifact{Path: "//gateway/service.yaml", Name: "gateway"}},
		},
	}

	if err := env.Update(deployConfig); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	if pushCalls != 0 {
		t.Fatalf("Update() push calls = %d, want 0", pushCalls)
	}
}

func TestDeploy_RunsPushBeforeApply(t *testing.T) {
	newBazelWorkspace(t, ".env")
	internalInit()

	env, err := Get("empty", "env")
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	deployConfig := &config.DeployConfig{
		Template: "deploy",
		App:      "grpc-hello-world",
		Desc:     "开发环境",
		URI:      "//deploy/grpc-hello-world/deploy.yaml",
		Services: []*config.DeployService{
			{Artifact: config.DeployArtifact{Path: "//service/service.yaml", Name: "service"}},
			{Artifact: config.DeployArtifact{Path: "//gateway/service.yaml", Name: "gateway"}},
		},
	}
	if err := env.Update(deployConfig); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	events := []string{}
	withImageResolver(t, &stubImageResolver{
		resolveFunc: func(context.Context, *config.DeployConfig, []*config.ServiceConfig) (map[string]string, error) {
			events = append(events, "push")
			return map[string]string{
				"//service:service_image": "registry.example.com/team/service@sha256:1111111111111111111111111111111111111111111111111111111111111111",
				"//gateway:gateway_image": "registry.example.com/team/gateway@sha256:2222222222222222222222222222222222222222222222222222222222222222",
			}, nil
		},
	})

	exec := &stubExecutor{
		applyFunc: func(ctx context.Context, objects *k8s.DeployObjects) error {
			events = append(events, "apply")
			if len(objects.Deployments) != 2 {
				t.Fatalf("Deploy() produced %d deployments, want 2", len(objects.Deployments))
			}

			gotImages := map[string]string{}
			for _, deployment := range objects.Deployments {
				gotImages[deployment.ServiceName] = deployment.Image
			}
			wantImages := map[string]string{
				"service": "registry.example.com/team/service@sha256:1111111111111111111111111111111111111111111111111111111111111111",
				"gateway": "registry.example.com/team/gateway@sha256:2222222222222222222222222222222222222222222222222222222222222222",
			}
			if !reflect.DeepEqual(gotImages, wantImages) {
				t.Fatalf("Apply() deployment images = %v, want %v", gotImages, wantImages)
			}
			return nil
		},
	}

	if err := env.Deploy(context.Background(), exec); err != nil {
		t.Fatalf("Deploy() failed: %v", err)
	}

	if !reflect.DeepEqual(events, []string{"push", "apply"}) {
		t.Fatalf("Deploy() events = %v, want [push apply]", events)
	}
}

func TestDeploy_CachesByPushTarget(t *testing.T) {
	newBazelWorkspace(t, "")
	internalInit()

	env := &DeployEnv{
		Profile: Profile{Name: "dev", App: "shared-app"},
		mainDeployConfig: &config.DeployConfig{
			App:      "shared-app",
			Template: "deploy",
			Services: []*config.DeployService{
				{Artifact: config.DeployArtifact{Path: "//svc/api.yaml", Name: "api"}},
				{Artifact: config.DeployArtifact{Path: "//svc/worker.yaml", Name: "worker"}},
			},
		},
		serviceConfigs: []*config.ServiceConfig{
			{
				URI:  "//svc/api.yaml",
				Name: "api",
				App:  "shared-app",
				Desc: "api service",
				Artifacts: []*config.ServiceArtifact{{
					Name:   "api",
					Type:   config.ServiceArtifactTypeDeployment,
					Target: "//shared:bundle_image",
					Ports:  []*config.ServiceArtifactPort{{Name: "http", Port: 8080}},
				}},
			},
			{
				URI:  "//svc/worker.yaml",
				Name: "worker",
				App:  "shared-app",
				Desc: "worker service",
				Artifacts: []*config.ServiceArtifact{{
					Name:   "worker",
					Type:   config.ServiceArtifactTypeDeployment,
					Target: "//shared:bundle_image",
					Ports:  []*config.ServiceArtifactPort{{Name: "http", Port: 9090}},
				}},
			},
		},
	}

	runCalls := []string{}
	withImageResolver(t, &defaultImageResolver{
		newRunner: func() (imagepush.Runner, error) {
			return &stubImagePushRunner{
				runFunc: func(_ context.Context, pushTarget string) (*imagepush.PushOutput, error) {
					runCalls = append(runCalls, pushTarget)
					return &imagepush.PushOutput{
						Repository: "registry.example.com/team/shared",
						Digest:     "sha256:3333333333333333333333333333333333333333333333333333333333333333",
					}, nil
				},
			}, nil
		},
	})

	exec := &stubExecutor{applyFunc: func(ctx context.Context, objects *k8s.DeployObjects) error {
		gotImages := map[string]string{}
		for _, deployment := range objects.Deployments {
			gotImages[deployment.ServiceName] = deployment.Image
		}
		wantImages := map[string]string{
			"api":    "registry.example.com/team/shared@sha256:3333333333333333333333333333333333333333333333333333333333333333",
			"worker": "registry.example.com/team/shared@sha256:3333333333333333333333333333333333333333333333333333333333333333",
		}
		if !reflect.DeepEqual(gotImages, wantImages) {
			t.Fatalf("Apply() deployment images = %v, want %v", gotImages, wantImages)
		}
		return nil
	}}

	if err := env.Deploy(context.Background(), exec); err != nil {
		t.Fatalf("Deploy() failed: %v", err)
	}
	if !reflect.DeepEqual(runCalls, []string{"//shared:bundle_image_push"}) {
		t.Fatalf("push targets = %v, want [//shared:bundle_image_push]", runCalls)
	}
}

func TestDeploy_StopsBeforeApplyOnPushFailure(t *testing.T) {
	newBazelWorkspace(t, ".env")
	internalInit()

	env, err := Get("empty", "env")
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	deployConfig := &config.DeployConfig{
		Template: "deploy",
		App:      "grpc-hello-world",
		Desc:     "开发环境",
		URI:      "//deploy/grpc-hello-world/deploy.yaml",
		Services: []*config.DeployService{
			{Artifact: config.DeployArtifact{Path: "//service/service.yaml", Name: "service"}},
			{Artifact: config.DeployArtifact{Path: "//gateway/service.yaml", Name: "gateway"}},
		},
	}
	if err := env.Update(deployConfig); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	pushErr := errors.New("push failed")
	withImageResolver(t, &stubImageResolver{
		resolveFunc: func(context.Context, *config.DeployConfig, []*config.ServiceConfig) (map[string]string, error) {
			return nil, pushErr
		},
	})

	applyCalled := false
	exec := &stubExecutor{
		applyFunc: func(context.Context, *k8s.DeployObjects) error {
			applyCalled = true
			return nil
		},
	}

	if err := env.Deploy(context.Background(), exec); !errors.Is(err, pushErr) {
		t.Fatalf("Deploy() err = %v, want %v", err, pushErr)
	}
	if applyCalled {
		t.Fatal("Deploy() called Apply() after push failure")
	}

	cached, err := Get("empty", "env")
	if err != nil {
		t.Fatalf("Get() after Deploy() failed: %v", err)
	}
	if cached.RemoteStatus != RemoteStatusPending {
		t.Fatalf("Deploy() remote_status = %q, want %q", cached.RemoteStatus, RemoteStatusPending)
	}
}

func TestDeployEnv_Update(t *testing.T) {
	tests := []struct {
		caseName string // description of this test case
		// Named input parameters for target function.
		name         string
		app          string
		deployConfig *config.DeployConfig
		want         *DeployEnv
		wantErr      bool
	}{
		{
			caseName: "正常更新",
			name:     "test_env",
			app:      "grpc-hello-world",
			deployConfig: &config.DeployConfig{
				Template: "deploy",
				App:      "grpc-hello-world",
				Desc:     "开发环境1111122222",
				URI:      "//deploy/grpc-hello-world/deploy_update.yaml",
				Services: []*config.DeployService{
					{
						Artifact: config.DeployArtifact{
							Path: "//service/service.yaml",
							Name: "service",
						},
					},
					{
						Artifact: config.DeployArtifact{
							Path: "//gateway/service1111.yaml",
							Name: "gateway",
						},
						HTTP: config.DeployHTTP{
							Hostnames: []string{"hello.liukexin.com"},
							Matches: []*config.DeployHTTPMatch{
								{
									Backend: "http",
									Path: config.DeployHTTPPathMatch{
										Type:  config.HTTPPathMatchTypePrefix,
										Value: "/v1",
									},
								},
							},
						},
					},
				},
			},
			want: &DeployEnv{
				Profile: Profile{
					Name:         "test_env",
					App:          "grpc-hello-world",
					RemoteStatus: RemoteStatusPending,
					MainConfig:   "grpc-hello-world__test_env__grpc-hello-world__deploy.yaml",
				},
				mainDeployConfig: &config.DeployConfig{
					Template: "deploy",
					App:      "grpc-hello-world",
					Desc:     "开发环境1111122222",
					URI:      "//deploy/grpc-hello-world/deploy_update.yaml",
					Services: []*config.DeployService{
						{
							Artifact: config.DeployArtifact{
								Path: "//service/service.yaml",
								Name: "service",
							},
						},
						{
							Artifact: config.DeployArtifact{
								Path: "//gateway/service1111.yaml",
								Name: "gateway",
							},
							HTTP: config.DeployHTTP{
								Hostnames: []string{"hello.liukexin.com"},
								Matches: []*config.DeployHTTPMatch{
									{
										Backend: "http",
										Path: config.DeployHTTPPathMatch{
											Type:  config.HTTPPathMatchTypePrefix,
											Value: "/v1",
										},
									},
								},
							},
						},
					},
				},
				serviceConfigs: []*config.ServiceConfig{
					{
						Name: "service",
						App:  "grpc-hello-world",
						Desc: "service config",
						URI:  "//service/service.yaml",
						Artifacts: []*config.ServiceArtifact{
							{
								Name:   "service",
								Type:   config.ServiceArtifactTypeDeployment,
								Target: "//service:service_image",
								Ports: []*config.ServiceArtifactPort{
									{
										Name: "grpc",
										Port: 50051,
									},
								},
							},
							{
								Name:   "service111",
								Type:   config.ServiceArtifactTypeDeployment,
								Target: "//service:service111_image",
								Ports: []*config.ServiceArtifactPort{
									{
										Name: "grpc",
										Port: 50052,
									},
								},
							},
						},
					},
					{
						Name: "gateway",
						App:  "grpc-hello-world",
						Desc: "gateway config112312",
						URI:  "//gateway/service1111.yaml",
						Artifacts: []*config.ServiceArtifact{
							{
								Name:   "gateway",
								Type:   config.ServiceArtifactTypeDeployment,
								Target: "//gateway:gateway_image_123",
								Ports: []*config.ServiceArtifactPort{
									{
										Name: "http",
										Port: 80,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			caseName: "artifact_not_found",
			name:     "test_env",
			app:      "grpc-hello-world",
			deployConfig: &config.DeployConfig{
				Template: "deploy",
				App:      "grpc-hello-world",
				Desc:     "开发环境",
				Services: []*config.DeployService{
					{
						Artifact: config.DeployArtifact{
							Path: "//service/service.yaml",
							Name: "service11111",
						},
					},
					{
						Artifact: config.DeployArtifact{
							Path: "//gateway/service.yaml",
							Name: "gateway",
						},
						HTTP: config.DeployHTTP{
							Hostnames: []string{"hello.liukexin.com"},
							Matches: []*config.DeployHTTPMatch{
								{
									Backend: "http",
									Path: config.DeployHTTPPathMatch{
										Type:  config.HTTPPathMatchTypePrefix,
										Value: "/v1",
									},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			caseName: "服务定义文件不存在",
			name:     "empty",
			app:      "env",
			deployConfig: &config.DeployConfig{
				Template: "deploy",
				App:      "grpc-hello-world",
				Desc:     "开发环境",
				Services: []*config.DeployService{
					{
						Artifact: config.DeployArtifact{
							Path: "//service/service44444.yaml",
							Name: "service",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			caseName: "更新配置为空",
			app:      "grpc-hello-world",
			name:     "test_env",
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.caseName, func(t *testing.T) {
			newBazelWorkspace(t, ".env")
			internalInit()
			nowTime := time.Now().UTC().Round(0)
			now = func() time.Time {
				return nowTime
			}
			if !tt.wantErr {
				tt.want.UpdatedAt = nowTime
			}

			env, err := Get(tt.name, tt.app)
			if err != nil {
				t.Fatalf("could not construct receiver type: %v", err)
			}
			gotErr := env.Update(tt.deployConfig)
			if tt.wantErr {
				if gotErr == nil {
					t.Fatal("Update() succeeded unexpectedly")
				}
				return
			}

			if gotErr != nil {
				t.Fatalf("Update() failed: %v", gotErr)
			}

			env, err = Get(tt.name, tt.app)
			if err != nil {
				t.Fatalf("could not construct receiver type: %v", err)
			}

			if !reflect.DeepEqual(env, tt.want) {
				t.Fatalf("Update() = %v, want %v", env, tt.want)
			}
		})
	}
}

func TestDeployEnv_Update_PersistsPendingStatusWithoutRemoteApply(t *testing.T) {
	tests := []struct {
		caseName     string
		name         string
		app          string
		deployConfig *config.DeployConfig
	}{
		{
			caseName: "远端应用先于本地缓存落盘",
			name:     "empty",
			app:      "env",
			deployConfig: &config.DeployConfig{
				Template: "deploy",
				App:      "grpc-hello-world",
				Desc:     "开发环境",
				URI:      "//deploy/grpc-hello-world/deploy.yaml",
				Services: []*config.DeployService{
					{
						Artifact: config.DeployArtifact{
							Path: "//service/service.yaml",
							Name: "service",
						},
					},
					{
						Artifact: config.DeployArtifact{
							Path: "//gateway/service.yaml",
							Name: "gateway",
						},
						HTTP: config.DeployHTTP{
							Hostnames: []string{"hello.liukexin.com"},
							Matches: []*config.DeployHTTPMatch{
								{
									Backend: "http",
									Path: config.DeployHTTPPathMatch{
										Type:  config.HTTPPathMatchTypePrefix,
										Value: "/v1",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.caseName, func(t *testing.T) {
			newBazelWorkspace(t, ".env")
			internalInit()

			env, err := Get(tt.name, tt.app)
			if err != nil {
				t.Fatalf("Get() failed: %v", err)
			}

			applyCalled := false
			exec := &stubExecutor{
				applyFunc: func(ctx context.Context, objects *k8s.DeployObjects) error {
					applyCalled = true
					return nil
				},
			}

			if err := env.Update(tt.deployConfig); err != nil {
				t.Fatalf("Update() failed: %v", err)
			}

			if applyCalled {
				t.Fatal("executor Apply() was called unexpectedly during Update")
			}

			cached, err := Get(tt.name, tt.app)
			if err != nil {
				t.Fatalf("Get() after Update() failed: %v", err)
			}
			if cached.MainConfig == "" {
				t.Fatal("profile cache was not persisted after Update()")
			}
			if cached.RemoteStatus != RemoteStatusPending {
				t.Fatalf("Update() remote_status = %q, want %q", cached.RemoteStatus, RemoteStatusPending)
			}

			withImageResolver(t, &stubImageResolver{
				resolveFunc: func(context.Context, *config.DeployConfig, []*config.ServiceConfig) (map[string]string, error) {
					return map[string]string{
						"//service:service_image": "registry.example.com/team/service@sha256:1111111111111111111111111111111111111111111111111111111111111111",
						"//gateway:gateway_image": "registry.example.com/team/gateway@sha256:2222222222222222222222222222222222222222222222222222222222222222",
					}, nil
				},
			})

			if err := env.Deploy(context.Background(), exec); err != nil {
				t.Fatalf("Deploy() failed: %v", err)
			}

			if !applyCalled {
				t.Fatal("executor Apply() was not called during Deploy")
			}

			deployedCache, err := Get(tt.name, tt.app)
			if err != nil {
				t.Fatalf("Get() after Deploy() failed: %v", err)
			}
			if deployedCache.RemoteStatus != RemoteStatusDeployed {
				t.Fatalf("Deploy() remote_status = %q, want %q", deployedCache.RemoteStatus, RemoteStatusDeployed)
			}
		})
	}
}

func TestDeployEnv_Delete_RemoteDeleteFailurePreservesLocalCache(t *testing.T) {
	tests := []struct {
		caseName string
		name     string
		app      string
	}{
		{
			caseName: "远端删除失败时保留本地缓存",
			name:     "test_env",
			app:      "grpc-hello-world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.caseName, func(t *testing.T) {
			dir := newBazelWorkspace(t, ".env")
			internalInit()

			env, err := Get(tt.name, tt.app)
			if err != nil {
				t.Fatalf("Get() failed: %v", err)
			}

			wantErr := errors.New("remote delete failed")
			deleteCalled := false
			exec := &stubExecutor{
				deleteFunc: func(ctx context.Context, app, environment string) error {
					deleteCalled = true
					if app != tt.app || environment != tt.name {
						t.Fatalf("Delete() got app=%q environment=%q, want app=%q environment=%q", app, environment, tt.app, tt.name)
					}
					return wantErr
				},
			}

			err = env.Delete(context.Background(), exec)
			if err == nil {
				t.Fatal("Delete() succeeded unexpectedly")
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("Delete() err = %v, want %v", err, wantErr)
			}
			if !deleteCalled {
				t.Fatal("executor Delete() was not called")
			}

			cached, err := Get(tt.name, tt.app)
			if err != nil {
				t.Fatalf("Get() after failed Delete() failed: %v", err)
			}
			if !cached.Equal(env) {
				t.Fatalf("Get() after failed Delete() = %v, want %v", cached, env)
			}
			if _, err := os.Stat(filepath.Join(dir, ".env", "grpc-hello-world__test_env.json")); err != nil {
				t.Fatalf("profile cache missing after failed remote Delete(): %v", err)
			}
		})
	}
}

func TestDeployEnv_BuildDeployObjects(t *testing.T) {
	env := &DeployEnv{
		Profile: Profile{Name: "dev", App: "grpc-hello-world"},
		mainDeployConfig: &config.DeployConfig{
			App:      "grpc-hello-world",
			Template: "deploy",
			Services: []*config.DeployService{
				{Artifact: config.DeployArtifact{Path: "//svc/service.yaml", Name: "service"}},
				{
					Artifact: config.DeployArtifact{Path: "//svc/gateway.yaml", Name: "gateway"},
					HTTP: config.DeployHTTP{
						Matches: []*config.DeployHTTPMatch{{
							Backend: "http",
							Path:    config.DeployHTTPPathMatch{Type: config.HTTPPathMatchTypePrefix, Value: "/v1"},
						}},
					},
				},
			},
		},
		serviceConfigs: []*config.ServiceConfig{
			{
				URI:  "//svc/service.yaml",
				Name: "service",
				App:  "grpc-hello-world",
				Desc: "grpc service",
				Artifacts: []*config.ServiceArtifact{{
					Name:   "service",
					Type:   config.ServiceArtifactTypeDeployment,
					Target: "//svc:service_image",
					Ports:  []*config.ServiceArtifactPort{{Name: "grpc", Port: 50051}},
				}},
			},
			{
				URI:  "//svc/gateway.yaml",
				Name: "gateway",
				App:  "grpc-hello-world",
				Desc: "gateway service",
				Artifacts: []*config.ServiceArtifact{{
					Name:   "gateway",
					Type:   config.ServiceArtifactTypeDeployment,
					Target: "//svc:gateway_image",
					Ports:  []*config.ServiceArtifactPort{{Name: "http", Port: 80}},
				}},
			},
		},
	}

	objects, err := env.BuildDeployObjects(map[string]string{
		"//svc:service_image": "registry.example.com/team/service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"//svc:gateway_image": "registry.example.com/team/gateway@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	if err != nil {
		t.Fatalf("BuildDeployObjects() failed: %v", err)
	}

	if len(objects.Deployments) != 2 || len(objects.HTTPRoutes) != 1 {
		t.Fatalf("unexpected object counts: deployments=%d routes=%d", len(objects.Deployments), len(objects.HTTPRoutes))
	}

	if objects.Deployments[0].EnvironmentName != "dev" || objects.Deployments[1].EnvironmentName != "dev" {
		t.Fatal("environment name was not propagated into deployment workloads")
	}

	gotImages := map[string]string{}
	for _, deployment := range objects.Deployments {
		gotImages[deployment.ServiceName] = deployment.Image
	}
	wantImages := map[string]string{
		"service": "registry.example.com/team/service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"gateway": "registry.example.com/team/gateway@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	if !reflect.DeepEqual(gotImages, wantImages) {
		t.Fatalf("BuildDeployObjects() deployment images = %v, want %v", gotImages, wantImages)
	}
}
