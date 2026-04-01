package config

import (
	"reflect"
	"testing"

	"dominion/tools/deploy/pkg/workspace"
)

func TestNewYAMLValidator(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		path    string
		wantErr bool
	}{
		{
			name: "成功加载 deploy schema 文件",
			path: "testdata/deploy.schema.json",
		},
		{
			name:    "schema 格式错误",
			path:    "testdata/deploy.schema.error.json",
			wantErr: true,
		},
		{
			name:    "文件不存在",
			path:    "testdata/deploy1.schema.json",
			wantErr: true,
		},
		{
			name: "成功加载 service schema 文件",
			path: "testdata/service.schema.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, gotErr := NewYAMLValidator(tt.path)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("NewYAMLValidator() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("NewYAMLValidator() succeeded unexpectedly")
			}
		})
	}
}

func TestParseDeployConfig(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		path    string
		want    *DeployConfig
		wantErr bool
	}{
		{
			name: "读取部署配置成功",
			path: "testdata/deploy.yaml",
			want: &DeployConfig{
				Template: "deploy",
				App:      "grpc-hello-world",
				Desc:     "开发环境",
				URI:      "//testdata/deploy.yaml",
				Services: []*DeployService{
					{
						Artifact: DeployArtifact{
							Path: "//testdata/service/service.yaml",
							Name: "service",
						},
					},
					{
						Artifact: DeployArtifact{
							Path: "//testdata/gateway/service.yaml",
							Name: "gateway",
						},
						HTTP: DeployHTTP{
							Hostnames: []string{"hello.liukexin.com"},
							Matches: []*DeployHTTPMatch{
								{
									Backend: "http",
									Path: DeployHTTPPathMatch{
										Type:  HTTPPathMatchTypePrefix,
										Value: "/v1",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:    "文件不存在",
			path:    "testdata/deploy1.yaml",
			wantErr: true,
		},
		{
			name:    "部署配置文件格式错误",
			path:    "testdata/deploy.error.yaml",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(workspace.WorkspaceKey, ".")
			Validator, err := NewYAMLValidator("testdata/deploy.schema.json")
			if err != nil {
				t.Fatal("NewYAMLValidator() failed unexpectedly")
			}
			RegisterDeployValidator(Validator)

			got, gotErr := ParseDeployConfig(tt.path)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("ParseDeployConfig() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("ParseDeployConfig() succeeded unexpectedly")
			}
			if !reflect.DeepEqual(tt.want, got) {
				t.Errorf("ParseDeployConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseServiceConfig(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		path    string
		want    *ServiceConfig
		wantErr bool
	}{
		{
			name: "读取服务配置成功",
			path: "testdata/service.yaml",
			want: &ServiceConfig{
				Name: "service",
				App:  "grpc-hello-world",
				Desc: "grpc hello world service",
				URI:  "//testdata/service.yaml",
				Artifacts: []*ServiceArtifact{
					{
						Name:   "service",
						Type:   ServiceArtifactTypeDeployment,
						Target: "//testdata:service_image",
						Ports: []*ServiceArtifactPort{
							{
								Name: "grpc",
								Port: 50051,
							},
						},
					},
				},
			},
		},
		{
			name:    "文件不存在",
			path:    "testdata/service1.yaml",
			wantErr: true,
		},
		{
			name:    "服务配置文件格式错误",
			path:    "testdata/service.error.yaml",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(workspace.WorkspaceKey, ".")
			Validator, err := NewYAMLValidator("testdata/service.schema.json")
			if err != nil {
				t.Fatal("NewYAMLValidator() failed unexpectedly")
			}
			RegisterServiceValidator(Validator)

			got, gotErr := ParseServiceConfig(tt.path)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("ParseServiceConfig() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("ParseServiceConfig() succeeded unexpectedly")
			}
			if !reflect.DeepEqual(tt.want, got) {
				t.Errorf("ParseServiceConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServiceConfig_GetArtifact(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for receiver constructor.
		path         string
		artifactName string
		want         *ServiceArtifact
		wantErr      bool
	}{
		{
			name:         "正常返回产物",
			path:         "testdata/service.yaml",
			artifactName: "service",
			want: &ServiceArtifact{
				Name:   "service",
				Type:   "deployment",
				Target: "//testdata:service_image",
				Ports: []*ServiceArtifactPort{
					{
						Name: "grpc",
						Port: 50051,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(workspace.WorkspaceKey, ".")
			Validator, err := NewYAMLValidator("testdata/service.schema.json")
			if err != nil {
				t.Fatal("NewYAMLValidator() failed unexpectedly")
			}
			RegisterServiceValidator(Validator)

			c, err := ParseServiceConfig(tt.path)
			if err != nil {
				t.Fatalf("could not construct receiver type: %v", err)
			}
			got, gotErr := c.GetArtifact(tt.artifactName)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("GetArtifact() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("GetArtifact() succeeded unexpectedly")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetArtifact() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_uriDir(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		uri  string
		want string
	}{
		{
			name: "常规多级目录",
			uri:  "//a/b/file.yaml",
			want: "//a/b",
		},
		{
			name: "根目录文件",
			uri:  "//file.yaml",
			want: "//",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uriDir(tt.uri)
			if got != tt.want {
				t.Errorf("uriDir() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_normalizeArtifactTarget(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		target    string
		configURI string
		want      string
		wantErr   bool
	}{
		{
			name:      "绝对 target（含空白）原样返回",
			target:    "  //pkg:image  ",
			configURI: "//x/y/service.yaml",
			want:      "//pkg:image",
		},
		{
			name:      "短标签拼接到配置目录",
			target:    " :image ",
			configURI: "//a/b/service.yaml",
			want:      "//a/b:image",
		},
		{
			name:      "非法 target",
			target:    "image",
			configURI: "//a/b/service.yaml",
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := normalizeArtifactTarget(tt.target, tt.configURI)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("normalizeArtifactTarget() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("normalizeArtifactTarget() succeeded unexpectedly")
			}
			if got != tt.want {
				t.Errorf("normalizeArtifactTarget() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_normalizeArtifactPath(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		artifactPath string
		configURI    string
		want         string
	}{
		{
			name:         "绝对路径（含空白）原样返回",
			artifactPath: "  //svc/path.yaml  ",
			configURI:    "//a/b/deploy.yaml",
			want:         "//svc/path.yaml",
		},
		{
			name:         "相对路径拼接到配置目录",
			artifactPath: "service/service.yaml",
			configURI:    "//a/b/deploy.yaml",
			want:         "//a/b/service/service.yaml",
		},
		{
			name:         "相对路径含空白先 trim 再拼接",
			artifactPath: "  service/service.yaml  ",
			configURI:    "//a/b/deploy.yaml",
			want:         "//a/b/service/service.yaml",
		},
		{
			name:         "根目录配置拼接相对路径",
			artifactPath: "service.yaml",
			configURI:    "//deploy.yaml",
			want:         "///service.yaml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeArtifactPath(tt.artifactPath, tt.configURI)
			if got != tt.want {
				t.Errorf("normalizeArtifactPath() = %v, want %v", got, tt.want)
			}
		})
	}
}
