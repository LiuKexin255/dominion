package config_test

import (
	"reflect"
	"testing"

	"dominion/tools/deploy/pkg/config"
)

func TestNewYAMLValidater(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		path    string
		wantErr bool
	}{
		{
			name: "成功加载 deploy schema 文件",
			path: "../../deploy.schema.json",
		},
		{
			name:    "schema 格式错误",
			path:    "testdata/deploy.schema.error.json",
			wantErr: true,
		},
		{
			name:    "文件不存在",
			path:    "../../deploy1.schema.json",
			wantErr: true,
		},
		{
			name: "成功加载 service schema 文件",
			path: "../../service.schema.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, gotErr := config.NewYAMLValidater(tt.path)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("NewYAMLValidater() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("NewYAMLValidater() succeeded unexpectedly")
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
			validater, err := config.NewYAMLValidater("../../deploy.schema.json")
			if err != nil {
				t.Fatal("NewYAMLValidater() failed unexpectedly")
			}
			config.RegisterDeployValidater(validater)

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
			validater, err := config.NewYAMLValidater("../../service.schema.json")
			if err != nil {
				t.Fatal("NewYAMLValidater() failed unexpectedly")
			}
			config.RegisterServiceValidater(validater)

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
