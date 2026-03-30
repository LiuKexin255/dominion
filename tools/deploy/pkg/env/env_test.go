package env

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"dominion/tools/deploy/pkg/config"
	"dominion/tools/deploy/pkg/workspace"
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

func initValidator(t *testing.T) {
	t.Helper()

	serviceValidator, err := config.NewYAMLValidator(filepath.Join("testdata", "service.schema.json"))
	if err != nil {
		t.Fatalf("config.NewYAMLValidator(service) failed: %v", err)
	}
	config.RegisterServiceValidator(serviceValidator)

	deployValidator, err := config.NewYAMLValidator(filepath.Join("testdata", "deploy.schema.json"))
	if err != nil {
		t.Fatalf("config.NewYAMLValidator(service) failed: %v", err)
	}
	config.RegisterDeployValidator(deployValidator)
}

func Test_currentEnvInfo(t *testing.T) {
	tests := []struct {
		name    string // description of this test case
		want    *currentEnvInfo
		wantErr bool
	}{
		{
			name: "读取和载入当前环境信息",
			want: &currentEnvInfo{
				Name: "test-Name",
				App:  "app-test",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.Setenv("BUILD_WORKSPACE_DIRECTORY", dir)

			internalInit()

			gotErr := saveCurrentEnvInfo(tt.want)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("saveCurrentEnvInfo() failed: %v", gotErr)
				}
				return
			}

			got, gotErr := loadCurrentEnvInfo()
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("loadCurrentEnvInfo() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("loadCurrentEnvInfo() succeeded unexpectedly")
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("loadCurrentEnvInfo() = %v, want %v", got, tt.want)
			}
		})
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
			// TODO: update the condition below to compare got with tt.want.
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
		ws       string
		want     *DeployEnv
		wantErr  bool
	}{
		{
			caseName: "创建新的环境",
			name:     "test_env",
			app:      "test_app",
			ws:       t.TempDir(),
			want: &DeployEnv{
				Profile: Profile{
					Name: "test_env",
					App:  "test_app",
				},
			},
		},
		{
			caseName: "环境已存在",
			ws:       "testdata",
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

			os.Setenv(workspace.WorkspaceKey, tt.ws)

			internalInit()
			initValidator(t)

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
				serviceArtifacts: []*config.ServiceArtifact{
					{
						Name:   "service",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: ":service_image",
						Ports: []*config.ServiceArtifactPort{
							{
								Name: "grpc",
								Port: 50051,
							},
						},
					},
					{
						Name:   "gateway",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: ":gateway_image",
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
			os.Setenv(workspace.WorkspaceKey, "testdata")
			internalInit()
			initValidator(t)

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
		name string
		dir  string
		want []*DeployEnv
	}{
		{
			name: "读取所有环境",
			dir:  filepath.Join("testdata", "list"),
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
					serviceArtifacts: []*config.ServiceArtifact{
						{
							Name:   "service",
							Type:   config.ServiceArtifactTypeDeployment,
							Target: ":service_image",
							Ports: []*config.ServiceArtifactPort{
								{
									Name: "grpc",
									Port: 50051,
								},
							},
						},
						{
							Name:   "gateway",
							Type:   config.ServiceArtifactTypeDeployment,
							Target: ":gateway_image",
							Ports: []*config.ServiceArtifactPort{
								{
									Name: "http",
									Port: 80,
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
					serviceArtifacts: []*config.ServiceArtifact{
						{
							Name:   "service",
							Type:   config.ServiceArtifactTypeDeployment,
							Target: ":service111_image",
							Ports: []*config.ServiceArtifactPort{
								{
									Name: "grpc",
									Port: 50052,
								},
							},
						},
						{
							Name:   "gateway",
							Type:   config.ServiceArtifactTypeDeployment,
							Target: ":gateway_image",
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
			name: "无环境返回空切片",
			dir:  t.TempDir(),
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv(workspace.WorkspaceKey, tt.dir)
			internalInit()
			initValidator(t)

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
			dir := t.TempDir()
			copyDir(t, filepath.Join("testdata", ".env"), filepath.Join(dir, ".env"))
			os.Setenv(workspace.WorkspaceKey, dir)
			internalInit()
			initValidator(t)

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

			curEnv, gotErr = Current()
			if gotErr != nil {
				t.Fatalf("Current() failed: %v", gotErr)
			}

			if !reflect.DeepEqual(env, curEnv) {
				t.Fatalf("Current() = %v, want %v", env, curEnv)
			}

			gotErr = curEnv.Delete()
			if gotErr != nil {
				t.Fatalf("Delete() failed: %v", gotErr)
			}

			curEnv, gotErr = Current()
			if gotErr == nil || !errors.Is(gotErr, ErrNotActive) {
				t.Fatalf("Current() after Delete() gotErr = %v, want %v", gotErr, ErrNotActive)
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
			dir := t.TempDir()
			copyDir(t, filepath.Join("testdata", ".env"), filepath.Join(dir, ".env"))
			os.Setenv(workspace.WorkspaceKey, dir)
			internalInit()
			initValidator(t)

			e, gotErr := Get(tt.name, tt.app)
			if gotErr != nil {
				t.Fatalf("Get() failed: %v", gotErr)
			}

			gotErr = e.Delete()
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

func TestDeployEnv_Update(t *testing.T) {
	tests := []struct {
		caseName string // description of this test case
		// Named input parameters for target function.
		name         string
		app          string
		deployConfig *config.DeployConfig
		want         *DeployEnv
		// wantArtifacts []*config.ServiceArtifact
		wantErr bool
	}{
		{
			caseName: "正常更新",
			name:     "test_env",
			app:      "grpc-hello-world",
			deployConfig: &config.DeployConfig{
				Template: "deploy",
				App:      "grpc-hello-world",
				Desc:     "开发环境1111122222",
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
					Name:       "test_env",
					App:        "grpc-hello-world",
					MainConfig: "grpc-hello-world__test_env__grpc-hello-world__deploy.yaml",
				},
				mainDeployConfig: &config.DeployConfig{
					Template: "deploy",
					App:      "grpc-hello-world",
					Desc:     "开发环境1111122222",
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
				serviceArtifacts: []*config.ServiceArtifact{
					{
						Name:   "service",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: ":service_image",
						Ports: []*config.ServiceArtifactPort{
							{
								Name: "grpc",
								Port: 50051,
							},
						},
					},
					{
						Name:   "gateway",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: ":gateway_image_123",
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
			dir := t.TempDir()
			copyDir(t, "testdata", dir)
			os.Setenv(workspace.WorkspaceKey, dir)

			internalInit()
			initValidator(t)
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

			// if !reflect.DeepEqual(env.serviceArtifacts, tt.wantArtifacts) {
			// 	t.Fatalf("Update() serviceArtifacts = %v, want %v", env.serviceArtifacts, tt.wantArtifacts)
			// }

			env, err = Get(tt.name, tt.app)
			if err != nil {
				t.Fatalf("could not construct receiver type: %v", err)
			}

			if !reflect.DeepEqual(env, tt.want) {
				t.Fatalf("Update() = %v, want %v", env.serviceArtifacts, tt.want.serviceArtifacts)
			}
		})
	}
}
