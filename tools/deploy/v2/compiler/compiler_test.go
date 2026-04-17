package compiler

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	deploy "dominion/projects/infra/deploy"
	"dominion/tools/deploy/pkg/config"
	"dominion/tools/deploy/pkg/imagepush"
	"dominion/tools/deploy/pkg/workspace"

	"google.golang.org/protobuf/proto"
)

const (
	testServiceAPath = "//tools/deploy/v2/compiler/testdata/service-a.yaml"
	testServiceBPath = "//tools/deploy/v2/compiler/testdata/service-b.yaml"
	testServiceCPath = "//tools/deploy/v2/compiler/testdata/service-c.yaml"
)

func TestCompile(t *testing.T) {
	tests := []struct {
		name           string
		deployConfig   *config.DeployConfig
		serviceConfigs map[string]*config.ServiceConfig
		imageResults   map[string]*imagepush.Result
		want           *deploy.EnvironmentDesiredState
		wantErr        string
	}{
		{
			name: "pure artifact config",
			deployConfig: &config.DeployConfig{
				Services: []*config.DeployService{{
					Artifact: config.DeployArtifact{Path: testServiceAPath, Name: "service-a"},
				}},
			},
			serviceConfigs: map[string]*config.ServiceConfig{
				testServiceAPath: {
					Name: "service-a",
					App:  "alpha",
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-a",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-a:image",
						TLS:    true,
						Ports: []*config.ServiceArtifactPort{{
							Name: "grpc",
							Port: 50051,
						}},
					}},
				},
			},
			imageResults: map[string]*imagepush.Result{
				"//apps/service-a:image": {URL: "registry.example.com/service-a", Dest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
			want: &deploy.EnvironmentDesiredState{
				Artifacts: []*deploy.ArtifactSpec{{
					Name:       "service-a",
					App:        "alpha",
					Image:      "registry.example.com/service-a@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					Replicas:   1,
					TlsEnabled: true,
					Ports: []*deploy.ArtifactPortSpec{{
						Name: "grpc",
						Port: 50051,
					}},
				}},
			},
		},
		{
			name: "pure infra config",
			deployConfig: &config.DeployConfig{
				Services: []*config.DeployService{{
					Infra: config.DeployInfra{
						Resource: "mongodb",
						Profile:  "dev-single",
						Name:     "mongo",
						App:      "alpha",
						Persistence: config.DeployInfraPersistence{
							Enabled: true,
						},
					},
				}},
			},
			want: &deploy.EnvironmentDesiredState{
				Infras: []*deploy.InfraSpec{{
					Resource: "mongodb",
					Profile:  "dev-single",
					Name:     "mongo",
					App:      "alpha",
					Persistence: &deploy.InfraPersistenceSpec{
						Enabled: true,
					},
				}},
			},
		},
		{
			name: "artifact with http route",
			deployConfig: &config.DeployConfig{
				Services: []*config.DeployService{{
					Artifact: config.DeployArtifact{Path: testServiceAPath, Name: "service-a"},
					HTTP: config.DeployHTTP{
						Hostnames: []string{"service-a.example.com"},
						Matches: []*config.DeployHTTPMatch{{
							Backend: "grpc",
							Path: config.DeployHTTPPathMatch{
								Type:  config.HTTPPathMatchTypePrefix,
								Value: "/v1",
							},
						}},
					},
				}},
			},
			serviceConfigs: map[string]*config.ServiceConfig{
				testServiceAPath: {
					Name: "service-a",
					App:  "alpha",
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-a",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-a:image",
						Ports: []*config.ServiceArtifactPort{{
							Name: "grpc",
							Port: 50051,
						}},
					}},
				},
			},
			imageResults: map[string]*imagepush.Result{
				"//apps/service-a:image": {URL: "registry.example.com/service-a", Dest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			},
			want: &deploy.EnvironmentDesiredState{
				Artifacts: []*deploy.ArtifactSpec{{
					Name:     "service-a",
					App:      "alpha",
					Image:    "registry.example.com/service-a@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					Replicas: 1,
					Ports: []*deploy.ArtifactPortSpec{{
						Name: "grpc",
						Port: 50051,
					}},
					Http: &deploy.ArtifactHTTPSpec{
						Hostnames: []string{"service-a.example.com"},
						Matches: []*deploy.HTTPRouteRule{{
							Backend: "grpc",
							Path: &deploy.HTTPPathRule{
								Type:  deploy.HTTPPathRuleType_HTTP_PATH_RULE_TYPE_PATH_PREFIX,
								Value: "/v1",
							},
						}},
					},
				}},
			},
		},
		{
			name: "artifact and infra mixed",
			deployConfig: &config.DeployConfig{
				Services: []*config.DeployService{
					{
						Infra: config.DeployInfra{
							Resource: "mongodb",
							Profile:  "dev-single",
							Name:     "mongo",
							App:      "alpha",
						},
					},
					{
						Artifact: config.DeployArtifact{Path: testServiceBPath, Name: "service-b"},
					},
				},
			},
			serviceConfigs: map[string]*config.ServiceConfig{
				testServiceBPath: {
					Name: "service-b",
					App:  "beta",
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-b",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-b:image",
						Ports: []*config.ServiceArtifactPort{{
							Name: "http",
							Port: 8080,
						}},
					}},
				},
			},
			imageResults: map[string]*imagepush.Result{
				"//apps/service-b:image": {URL: "registry.example.com/service-b", Dest: "sha256:cccccccccccccccccccccccccccccccc"},
			},
			want: &deploy.EnvironmentDesiredState{
				Artifacts: []*deploy.ArtifactSpec{{
					Name:     "service-b",
					App:      "beta",
					Image:    "registry.example.com/service-b@sha256:cccccccccccccccccccccccccccccccc",
					Replicas: 1,
					Ports: []*deploy.ArtifactPortSpec{{
						Name: "http",
						Port: 8080,
					}},
				}},
				Infras: []*deploy.InfraSpec{{
					Resource: "mongodb",
					Profile:  "dev-single",
					Name:     "mongo",
					App:      "alpha",
				}},
			},
		},
		{
			name: "http route backend referencing non existent service",
			deployConfig: &config.DeployConfig{
				Services: []*config.DeployService{{
					Artifact: config.DeployArtifact{Path: testServiceAPath, Name: "service-a"},
					HTTP: config.DeployHTTP{
						Hostnames: []string{"service-a.example.com"},
						Matches: []*config.DeployHTTPMatch{{
							Backend: "missing-port",
							Path: config.DeployHTTPPathMatch{
								Type:  config.HTTPPathMatchTypePrefix,
								Value: "/v1",
							},
						}},
					},
				}},
			},
			serviceConfigs: map[string]*config.ServiceConfig{
				testServiceAPath: {
					Name: "service-a",
					App:  "alpha",
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-a",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-a:image",
						Ports: []*config.ServiceArtifactPort{{
							Name: "grpc",
							Port: 50051,
						}},
					}},
				},
			},
			imageResults: map[string]*imagepush.Result{
				"//apps/service-a:image": {URL: "registry.example.com/service-a", Dest: "sha256:dddddddddddddddddddddddddddddddd"},
			},
			wantErr: "http backend missing-port not found in service service-a",
		},
		{
			name: "empty services",
			deployConfig: &config.DeployConfig{
				Services: nil,
			},
			want: &deploy.EnvironmentDesiredState{},
		},
		{
			name: "multiple services different images",
			deployConfig: &config.DeployConfig{
				Services: []*config.DeployService{
					{Artifact: config.DeployArtifact{Path: testServiceAPath, Name: "service-a"}},
					{Artifact: config.DeployArtifact{Path: testServiceCPath, Name: "service-c"}},
				},
			},
			serviceConfigs: map[string]*config.ServiceConfig{
				testServiceAPath: {
					Name: "service-a",
					App:  "alpha",
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-a",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-a:image",
						Ports: []*config.ServiceArtifactPort{{
							Name: "grpc",
							Port: 50051,
						}},
					}},
				},
				testServiceCPath: {
					Name: "service-c",
					App:  "gamma",
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-c",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-c:image",
						Ports: []*config.ServiceArtifactPort{{
							Name: "http",
							Port: 8081,
						}},
					}},
				},
			},
			imageResults: map[string]*imagepush.Result{
				"//apps/service-a:image": {URL: "registry.example.com/service-a", Dest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},
				"//apps/service-c:image": {URL: "registry.example.com/service-c", Dest: "sha256:ffffffffffffffffffffffffffffffff"},
			},
			want: &deploy.EnvironmentDesiredState{
				Artifacts: []*deploy.ArtifactSpec{
					{
						Name:     "service-a",
						App:      "alpha",
						Image:    "registry.example.com/service-a@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
						Replicas: 1,
						Ports: []*deploy.ArtifactPortSpec{{
							Name: "grpc",
							Port: 50051,
						}},
					},
					{
						Name:     "service-c",
						App:      "gamma",
						Image:    "registry.example.com/service-c@sha256:ffffffffffffffffffffffffffffffff",
						Replicas: 1,
						Ports: []*deploy.ArtifactPortSpec{{
							Name: "http",
							Port: 8081,
						}},
					},
				},
			},
		},
		{
			name: "service ports merged with artifact ports",
			deployConfig: &config.DeployConfig{
				Services: []*config.DeployService{{
					Artifact: config.DeployArtifact{Path: testServiceAPath, Name: "service-a"},
				}},
			},
			serviceConfigs: map[string]*config.ServiceConfig{
				testServiceAPath: {
					Name:  "service-a",
					App:   "alpha",
					Ports: []*config.ServiceArtifactPort{{Name: "admin", Port: 9090}},
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-a",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-a:image",
						Ports: []*config.ServiceArtifactPort{{
							Name: "grpc",
							Port: 50051,
						}},
					}},
				},
			},
			imageResults: map[string]*imagepush.Result{
				"//apps/service-a:image": {URL: "registry.example.com/service-a", Dest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
			want: &deploy.EnvironmentDesiredState{
				Artifacts: []*deploy.ArtifactSpec{{
					Name:     "service-a",
					App:      "alpha",
					Image:    "registry.example.com/service-a@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					Replicas: 1,
					Ports: []*deploy.ArtifactPortSpec{
						{Name: "admin", Port: 9090},
						{Name: "grpc", Port: 50051},
					},
				}},
			},
		},
		{
			name: "service ports conflict with artifact ports",
			deployConfig: &config.DeployConfig{
				Services: []*config.DeployService{{
					Artifact: config.DeployArtifact{Path: testServiceAPath, Name: "service-a"},
				}},
			},
			serviceConfigs: map[string]*config.ServiceConfig{
				testServiceAPath: {
					Name:  "service-a",
					App:   "alpha",
					Ports: []*config.ServiceArtifactPort{{Name: "grpc", Port: 9090}},
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-a",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-a:image",
						Ports: []*config.ServiceArtifactPort{{
							Name: "grpc",
							Port: 50051,
						}},
					}},
				},
			},
			imageResults: map[string]*imagepush.Result{
				"//apps/service-a:image": {URL: "registry.example.com/service-a", Dest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
			wantErr: "duplicate port name \"grpc\"",
		},
		{
			name: "service ports only, artifact has no ports",
			deployConfig: &config.DeployConfig{
				Services: []*config.DeployService{{
					Artifact: config.DeployArtifact{Path: testServiceAPath, Name: "service-a"},
				}},
			},
			serviceConfigs: map[string]*config.ServiceConfig{
				testServiceAPath: {
					Name:  "service-a",
					App:   "alpha",
					Ports: []*config.ServiceArtifactPort{{Name: "admin", Port: 9090}},
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-a",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-a:image",
						Ports:  nil,
					}},
				},
			},
			imageResults: map[string]*imagepush.Result{
				"//apps/service-a:image": {URL: "registry.example.com/service-a", Dest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
			want: &deploy.EnvironmentDesiredState{
				Artifacts: []*deploy.ArtifactSpec{{
					Name:     "service-a",
					App:      "alpha",
					Image:    "registry.example.com/service-a@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					Replicas: 1,
					Ports: []*deploy.ArtifactPortSpec{
						{Name: "admin", Port: 9090},
					},
				}},
			},
		},
		{
			name: "empty service ports, artifact has ports",
			deployConfig: &config.DeployConfig{
				Services: []*config.DeployService{{
					Artifact: config.DeployArtifact{Path: testServiceAPath, Name: "service-a"},
				}},
			},
			serviceConfigs: map[string]*config.ServiceConfig{
				testServiceAPath: {
					Name:  "service-a",
					App:   "alpha",
					Ports: nil,
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-a",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-a:image",
						Ports: []*config.ServiceArtifactPort{{
							Name: "grpc",
							Port: 50051,
						}},
					}},
				},
			},
			imageResults: map[string]*imagepush.Result{
				"//apps/service-a:image": {URL: "registry.example.com/service-a", Dest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
			want: &deploy.EnvironmentDesiredState{
				Artifacts: []*deploy.ArtifactSpec{{
					Name:     "service-a",
					App:      "alpha",
					Image:    "registry.example.com/service-a@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					Replicas: 1,
					Ports: []*deploy.ArtifactPortSpec{{
						Name: "grpc",
						Port: 50051,
					}},
				}},
			},
		},
		{
			name: "http backend referencing service port",
			deployConfig: &config.DeployConfig{
				Services: []*config.DeployService{{
					Artifact: config.DeployArtifact{Path: testServiceAPath, Name: "service-a"},
					HTTP: config.DeployHTTP{
						Hostnames: []string{"service-a.example.com"},
						Matches: []*config.DeployHTTPMatch{{
							Backend: "admin",
							Path: config.DeployHTTPPathMatch{
								Type:  config.HTTPPathMatchTypePrefix,
								Value: "/admin",
							},
						}},
					},
				}},
			},
			serviceConfigs: map[string]*config.ServiceConfig{
				testServiceAPath: {
					Name:  "service-a",
					App:   "alpha",
					Ports: []*config.ServiceArtifactPort{{Name: "admin", Port: 9090}},
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-a",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-a:image",
						Ports: []*config.ServiceArtifactPort{{
							Name: "grpc",
							Port: 50051,
						}},
					}},
				},
			},
			imageResults: map[string]*imagepush.Result{
				"//apps/service-a:image": {URL: "registry.example.com/service-a", Dest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
			want: &deploy.EnvironmentDesiredState{
				Artifacts: []*deploy.ArtifactSpec{{
					Name:     "service-a",
					App:      "alpha",
					Image:    "registry.example.com/service-a@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					Replicas: 1,
					Ports: []*deploy.ArtifactPortSpec{
						{Name: "admin", Port: 9090},
						{Name: "grpc", Port: 50051},
					},
					Http: &deploy.ArtifactHTTPSpec{
						Hostnames: []string{"service-a.example.com"},
						Matches: []*deploy.HTTPRouteRule{{
							Backend: "admin",
							Path: &deploy.HTTPPathRule{
								Type:  deploy.HTTPPathRuleType_HTTP_PATH_RULE_TYPE_PATH_PREFIX,
								Value: "/admin",
							},
						}},
					},
				}},
			},
		},
		{
			name: "multiple artifacts share service ports",
			deployConfig: &config.DeployConfig{
				Services: []*config.DeployService{
					{Artifact: config.DeployArtifact{Path: testServiceAPath, Name: "service-a"}},
					{Artifact: config.DeployArtifact{Path: testServiceBPath, Name: "service-b"}},
				},
			},
			serviceConfigs: map[string]*config.ServiceConfig{
				testServiceAPath: {
					Name:  "service-a",
					App:   "alpha",
					Ports: []*config.ServiceArtifactPort{{Name: "admin", Port: 9090}},
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-a",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-a:image",
						Ports: []*config.ServiceArtifactPort{{
							Name: "grpc",
							Port: 50051,
						}},
					}},
				},
				testServiceBPath: {
					Name:  "service-b",
					App:   "beta",
					Ports: []*config.ServiceArtifactPort{{Name: "admin", Port: 9090}},
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-b",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-b:image",
						Ports:  nil,
					}},
				},
			},
			imageResults: map[string]*imagepush.Result{
				"//apps/service-a:image": {URL: "registry.example.com/service-a", Dest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
				"//apps/service-b:image": {URL: "registry.example.com/service-b", Dest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			},
			want: &deploy.EnvironmentDesiredState{
				Artifacts: []*deploy.ArtifactSpec{
					{
						Name:     "service-a",
						App:      "alpha",
						Image:    "registry.example.com/service-a@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						Replicas: 1,
						Ports: []*deploy.ArtifactPortSpec{
							{Name: "admin", Port: 9090},
							{Name: "grpc", Port: 50051},
						},
					},
					{
						Name:     "service-b",
						App:      "beta",
						Image:    "registry.example.com/service-b@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
						Replicas: 1,
						Ports: []*deploy.ArtifactPortSpec{
							{Name: "admin", Port: 9090},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Compile(tt.deployConfig, tt.serviceConfigs, tt.imageResults)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Compile() expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Compile() error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("Compile() unexpected error: %v", err)
			}
			if !proto.Equal(tt.want, got) {
				t.Fatalf("Compile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveArtifactTargets(t *testing.T) {
	deployConfig := &config.DeployConfig{
		Services: []*config.DeployService{
			{Artifact: config.DeployArtifact{Path: testServiceAPath, Name: "service-a"}},
			{Infra: config.DeployInfra{Resource: "mongodb", Name: "mongo"}},
			{Artifact: config.DeployArtifact{Path: testServiceBPath, Name: "service-b"}},
			{Artifact: config.DeployArtifact{Path: testServiceAPath, Name: "service-a"}},
		},
	}

	serviceConfigs := map[string]*config.ServiceConfig{
		testServiceAPath: {
			Name: "service-a",
			Artifacts: []*config.ServiceArtifact{{
				Name:   "service-a",
				Type:   config.ServiceArtifactTypeDeployment,
				Target: "//apps/service-a:image",
			}},
		},
		testServiceBPath: {
			Name: "service-b",
			Artifacts: []*config.ServiceArtifact{{
				Name:   "service-b",
				Type:   config.ServiceArtifactTypeDeployment,
				Target: "//apps/service-b:image",
			}},
		},
	}

	got, err := ResolveArtifactTargets(deployConfig, serviceConfigs)
	if err != nil {
		t.Fatalf("ResolveArtifactTargets() unexpected error: %v", err)
	}

	want := []string{"//apps/service-a:image", "//apps/service-b:image"}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("ResolveArtifactTargets() = %v, want %v", got, want)
	}
}

func TestReadServiceConfigs(t *testing.T) {
	newCompilerWorkspace(t)

	deployConfig := &config.DeployConfig{
		Services: []*config.DeployService{
			{Artifact: config.DeployArtifact{Path: testServiceAPath, Name: "service-a"}},
			{Infra: config.DeployInfra{Resource: "mongodb", Name: "mongo"}},
			{Artifact: config.DeployArtifact{Path: testServiceBPath, Name: "service-b"}},
		},
	}

	got, err := ReadServiceConfigs(deployConfig)
	if err != nil {
		t.Fatalf("ReadServiceConfigs() unexpected error: %v", err)
	}

	want := map[string]*config.ServiceConfig{
		testServiceAPath: mustParseServiceConfig(t, testServiceAPath),
		testServiceBPath: mustParseServiceConfig(t, testServiceBPath),
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("ReadServiceConfigs() = %#v, want %#v", got, want)
	}
}

func mustParseServiceConfig(t *testing.T, uri string) *config.ServiceConfig {
	t.Helper()

	serviceConfig, err := config.ParseServiceConfig(filepath.Join(workspace.MustRoot(), strings.TrimPrefix(uri, workspace.WorkspacePathPrefix)))
	if err != nil {
		t.Fatalf("ParseServiceConfig(%q) failed: %v", uri, err)
	}

	return serviceConfig
}

func newCompilerWorkspace(t *testing.T) string {
	t.Helper()

	srcRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	copyDir(t, filepath.Join(srcRoot, "testdata"), filepath.Join(root, "tools", "deploy", "v2", "compiler", "testdata"))
	withWorkingDir(t, root)

	return root
}

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

func TestCompile_ReplicasFromConfig(t *testing.T) {
	tests := []struct {
		name           string
		deployConfig   *config.DeployConfig
		serviceConfigs map[string]*config.ServiceConfig
		imageResults   map[string]*imagepush.Result
		wantReplicas   int32
	}{
		{
			name: "config specifies replicas 3",
			deployConfig: &config.DeployConfig{
				Services: []*config.DeployService{{
					Artifact: config.DeployArtifact{
						Path:     testServiceAPath,
						Name:     "service-a",
						Replicas: 3,
					},
				}},
			},
			serviceConfigs: map[string]*config.ServiceConfig{
				testServiceAPath: {
					Name: "service-a",
					App:  "alpha",
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-a",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-a:image",
						Ports: []*config.ServiceArtifactPort{{
							Name: "grpc",
							Port: 50051,
						}},
					}},
				},
			},
			imageResults: map[string]*imagepush.Result{
				"//apps/service-a:image": {URL: "registry.example.com/service-a", Dest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
			wantReplicas: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Compile(tt.deployConfig, tt.serviceConfigs, tt.imageResults)
			if err != nil {
				t.Fatalf("Compile() unexpected error: %v", err)
			}
			if len(got.Artifacts) != 1 {
				t.Fatalf("Compile() returned %d artifacts, want 1", len(got.Artifacts))
			}
			if got.Artifacts[0].Replicas != tt.wantReplicas {
				t.Errorf("Compile() Replicas = %d, want %d", got.Artifacts[0].Replicas, tt.wantReplicas)
			}
		})
	}
}

func TestCompile_DefaultReplicas(t *testing.T) {
	tests := []struct {
		name           string
		deployConfig   *config.DeployConfig
		serviceConfigs map[string]*config.ServiceConfig
		imageResults   map[string]*imagepush.Result
		wantReplicas   int32
	}{
		{
			name: "config without replicas defaults to 1",
			deployConfig: &config.DeployConfig{
				Services: []*config.DeployService{{
					Artifact: config.DeployArtifact{
						Path: testServiceAPath,
						Name: "service-a",
					},
				}},
			},
			serviceConfigs: map[string]*config.ServiceConfig{
				testServiceAPath: {
					Name: "service-a",
					App:  "alpha",
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service-a",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//apps/service-a:image",
						Ports: []*config.ServiceArtifactPort{{
							Name: "grpc",
							Port: 50051,
						}},
					}},
				},
			},
			imageResults: map[string]*imagepush.Result{
				"//apps/service-a:image": {URL: "registry.example.com/service-a", Dest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
			wantReplicas: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Compile(tt.deployConfig, tt.serviceConfigs, tt.imageResults)
			if err != nil {
				t.Fatalf("Compile() unexpected error: %v", err)
			}
			if len(got.Artifacts) != 1 {
				t.Fatalf("Compile() returned %d artifacts, want 1", len(got.Artifacts))
			}
			if got.Artifacts[0].Replicas != tt.wantReplicas {
				t.Errorf("Compile() Replicas = %d, want %d", got.Artifacts[0].Replicas, tt.wantReplicas)
			}
		})
	}
}

func intPtr(v int) *int {
	return &v
}
