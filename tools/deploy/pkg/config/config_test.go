package config_test

import (
	"reflect"
	"testing"

	"dominion/tools/deploy/pkg/config"
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
			_, gotErr := config.NewYAMLValidator(tt.path)
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
		want    *config.DeployConfig
		wantErr bool
	}{
		{
			name: "读取部署配置成功",
			path: "testdata/deploy.yaml",
			want: &config.DeployConfig{
				Template: "deploy",
				App:      "grpc-hello-world",
				Desc:     "开发环境",
				Services: []*config.DeployService{
					{
						Artifact: config.DeployArtifact{
							Path: "service/service.yaml",
							Name: "service",
						},
					},
					{
						Artifact: config.DeployArtifact{
							Path: "gateway/service.yaml",
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
			Validator, err := config.NewYAMLValidator("testdata/deploy.schema.json")
			if err != nil {
				t.Fatal("NewYAMLValidator() failed unexpectedly")
			}
			config.RegisterDeployValidator(Validator)

			got, gotErr := config.ParseDeployConfig(tt.path)
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
		want    *config.ServiceConfig
		wantErr bool
	}{
		{
			name: "读取服务配置成功",
			path: "testdata/service.yaml",
			want: &config.ServiceConfig{
				Name: "service",
				App:  "grpc-hello-world",
				Desc: "grpc hello world service",
				Artifacts: []*config.ServiceArtifact{
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
			Validator, err := config.NewYAMLValidator("testdata/service.schema.json")
			if err != nil {
				t.Fatal("NewYAMLValidator() failed unexpectedly")
			}
			config.RegisterServiceValidator(Validator)

			got, gotErr := config.ParseServiceConfig(tt.path)
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
		want         *config.ServiceArtifact
		wantErr      bool
	}{
		{
			name:         "正常返回产物",
			path:         "testdata/service.yaml",
			artifactName: "service",
			want: &config.ServiceArtifact{
				Name:   "service",
				Type:   "deployment",
				Target: ":service_image",
				Ports: []*config.ServiceArtifactPort{
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
			Validator, err := config.NewYAMLValidator("testdata/service.schema.json")
			if err != nil {
				t.Fatal("NewYAMLValidator() failed unexpectedly")
			}
			config.RegisterServiceValidator(Validator)

			c, err := config.ParseServiceConfig(tt.path)
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
			// TODO: update the condition below to compare got with tt.want.
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetArtifact() = %v, want %v", got, tt.want)
			}
		})
	}
}
