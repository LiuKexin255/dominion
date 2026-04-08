package k8s

import (
	"strings"
	"testing"

	"dominion/tools/deploy/pkg/config"
)

func TestNewDeployObjects_SingleService(t *testing.T) {
	deployCfg := &config.DeployConfig{
		App:      "grpc-hello-world",
		Template: "deploy",
		Desc:     "dev",
		Services: []*config.DeployService{
			{
				Artifact: config.DeployArtifact{Path: "//svc/service.yaml", Name: "service"},
			},
		},
	}

	serviceConfigs := []*config.ServiceConfig{
		{
			URI:  "//svc/service.yaml",
			Name: "service",
			App:  "grpc-hello-world",
			Desc: "grpc service",
			Artifacts: []*config.ServiceArtifact{{
				Name:   "service",
				Type:   config.ServiceArtifactTypeDeployment,
				Target: "//some/path:service_image",
				Ports:  []*config.ServiceArtifactPort{{Name: "grpc", Port: 50051}},
			}},
		},
	}

	objects, err := NewDeployObjects(deployCfg, serviceConfigs, "dev", map[string]string{
		"//some/path:service_image": "registry.example.com/team/service@sha256:1111111111111111111111111111111111111111111111111111111111111111",
	})
	if err != nil {
		t.Fatalf("NewDeployObjects() failed: %v", err)
	}

	if len(objects.Deployments) != 1 || len(objects.Services) != 1 || len(objects.HTTPRoutes) != 0 {
		t.Fatalf("unexpected object counts: deployments=%d services=%d routes=%d", len(objects.Deployments), len(objects.Services), len(objects.HTTPRoutes))
	}
	if objects.Deployments[0].EnvironmentName != "dev" {
		t.Fatal("environment name was not propagated into deployment workload")
	}
	if objects.Deployments[0].Image != "registry.example.com/team/service@sha256:1111111111111111111111111111111111111111111111111111111111111111" {
		t.Fatalf("deployment image = %q, want injected resolved image", objects.Deployments[0].Image)
	}
}

func TestNewDeployObjects_MultipleServices(t *testing.T) {
	deployCfg := &config.DeployConfig{
		App:      "grpc-hello-world",
		Template: "deploy",
		Desc:     "dev",
		Services: []*config.DeployService{
			{
				Artifact: config.DeployArtifact{Path: "//svc/service.yaml", Name: "service"},
			},
			{
				Artifact: config.DeployArtifact{Path: "//svc/gateway.yaml", Name: "gateway"},
				HTTP: config.DeployHTTP{
					Hostnames: []string{"hello.example.com"},
					Matches: []*config.DeployHTTPMatch{{
						Backend: "http",
						Path:    config.DeployHTTPPathMatch{Type: config.HTTPPathMatchTypePrefix, Value: "/v1"},
					}},
				},
			},
		},
	}

	serviceConfigs := []*config.ServiceConfig{
		{
			URI:  "//svc/service.yaml",
			Name: "service",
			App:  "grpc-hello-world",
			Desc: "grpc service",
			Artifacts: []*config.ServiceArtifact{{
				Name:   "service",
				Type:   config.ServiceArtifactTypeDeployment,
				Target: "//some/path:service_image",
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
				Target: "//some/path:gateway_image",
				Ports:  []*config.ServiceArtifactPort{{Name: "http", Port: 80}},
			}},
		},
	}

	objects, err := NewDeployObjects(deployCfg, serviceConfigs, "dev", map[string]string{
		"//some/path:service_image": "registry.example.com/team/service@sha256:1111111111111111111111111111111111111111111111111111111111111111",
		"//some/path:gateway_image": "registry.example.com/team/gateway@sha256:2222222222222222222222222222222222222222222222222222222222222222",
	})
	if err != nil {
		t.Fatalf("NewDeployObjects() failed: %v", err)
	}

	if len(objects.Deployments) != 2 || len(objects.Services) != 2 || len(objects.HTTPRoutes) != 1 {
		t.Fatalf("unexpected object counts: deployments=%d services=%d routes=%d", len(objects.Deployments), len(objects.Services), len(objects.HTTPRoutes))
	}
	if objects.Deployments[0].EnvironmentName != "dev" || objects.Deployments[1].EnvironmentName != "dev" {
		t.Fatal("environment name was not propagated into deployment workloads")
	}
}

func TestNewDeployObjects_ServiceConfigOrderMismatch(t *testing.T) {
	deployCfg := &config.DeployConfig{
		Services: []*config.DeployService{
			{Artifact: config.DeployArtifact{Path: "//svc/service.yaml", Name: "service"}},
			{Artifact: config.DeployArtifact{Path: "//svc/gateway.yaml", Name: "gateway"}},
		},
	}

	serviceConfigs := []*config.ServiceConfig{
		{
			URI:  "//svc/gateway.yaml",
			Name: "gateway",
			App:  "grpc-hello-world",
			Desc: "gateway service",
			Artifacts: []*config.ServiceArtifact{{
				Name:   "gateway",
				Type:   config.ServiceArtifactTypeDeployment,
				Target: "//some/path:gateway_image",
				Ports:  []*config.ServiceArtifactPort{{Name: "http", Port: 80}},
			}},
		},
		{
			URI:  "//svc/service.yaml",
			Name: "service",
			App:  "grpc-hello-world",
			Desc: "grpc service",
			Artifacts: []*config.ServiceArtifact{{
				Name:   "service",
				Type:   config.ServiceArtifactTypeDeployment,
				Target: "//some/path:service_image",
				Ports:  []*config.ServiceArtifactPort{{Name: "grpc", Port: 50051}},
			}},
		},
	}

	objects, err := NewDeployObjects(deployCfg, serviceConfigs, "dev", map[string]string{
		"//some/path:service_image": "registry.example.com/team/service@sha256:1111111111111111111111111111111111111111111111111111111111111111",
		"//some/path:gateway_image": "registry.example.com/team/gateway@sha256:2222222222222222222222222222222222222222222222222222222222222222",
	})
	if err != nil {
		t.Fatalf("NewDeployObjects() failed: %v", err)
	}

	if len(objects.Deployments) != 2 || len(objects.Services) != 2 {
		t.Fatalf("unexpected object counts: deployments=%d services=%d", len(objects.Deployments), len(objects.Services))
	}

	deploymentNames := make(map[string]bool)
	for _, d := range objects.Deployments {
		deploymentNames[d.ServiceName] = true
	}
	if !deploymentNames["service"] || !deploymentNames["gateway"] {
		t.Fatalf("deployments 未正确匹配 service configs，got: %v", deploymentNames)
	}

	for _, d := range objects.Deployments {
		if d.ServiceName == "service" {
			if len(d.Ports) != 1 || d.Ports[0].Port != 50051 {
				t.Fatalf("service deployment 端口不匹配，expected grpc:50051, got: %v", d.Ports)
			}
		}
		if d.ServiceName == "gateway" {
			if len(d.Ports) != 1 || d.Ports[0].Port != 80 {
				t.Fatalf("gateway deployment 端口不匹配，expected http:80, got: %v", d.Ports)
			}
		}
	}
}

func TestNewDeployObjects_ErrorCases(t *testing.T) {
	tests := []struct {
		name           string
		deployCfg      *config.DeployConfig
		serviceConfigs []*config.ServiceConfig
		envName        string
		wantErr        bool
		errContains    string
	}{
		{
			name: "URI not found",
			deployCfg: &config.DeployConfig{
				Services: []*config.DeployService{
					{Artifact: config.DeployArtifact{Path: "//svc/not-found.yaml", Name: "service"}},
				},
			},
			serviceConfigs: []*config.ServiceConfig{
				{
					URI:  "//svc/service.yaml",
					Name: "service",
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//some/path:service_image",
					}},
				},
			},
			envName:     "dev",
			wantErr:     true,
			errContains: "未找到",
		},
		{
			name: "artifact name not found in matched config",
			deployCfg: &config.DeployConfig{
				Services: []*config.DeployService{
					{Artifact: config.DeployArtifact{Path: "//svc/a.yaml", Name: "gateway"}},
				},
			},
			serviceConfigs: []*config.ServiceConfig{
				{
					URI:  "//svc/a.yaml",
					Name: "service",
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//some/path:service_image",
					}},
				},
			},
			envName:     "dev",
			wantErr:     true,
			errContains: "未找到",
		},
		{
			name: "duplicate service config URI",
			deployCfg: &config.DeployConfig{
				Services: []*config.DeployService{
					{Artifact: config.DeployArtifact{Path: "//svc/a.yaml", Name: "service"}},
				},
			},
			serviceConfigs: []*config.ServiceConfig{
				{
					URI:  "//svc/a.yaml",
					Name: "service1",
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//some/path:service1_image",
					}},
				},
				{
					URI:  "//svc/a.yaml",
					Name: "service2",
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//some/path:service2_image",
					}},
				},
			},
			envName:     "dev",
			wantErr:     true,
			errContains: "重复",
		},
		{
			name: "empty service config URI",
			deployCfg: &config.DeployConfig{
				Services: []*config.DeployService{
					{Artifact: config.DeployArtifact{Path: "//svc/a.yaml", Name: "service"}},
				},
			},
			serviceConfigs: []*config.ServiceConfig{
				{
					URI:  "",
					Name: "service",
					Artifacts: []*config.ServiceArtifact{{
						Name:   "service",
						Type:   config.ServiceArtifactTypeDeployment,
						Target: "//some/path:service_image",
					}},
				},
			},
			envName:     "dev",
			wantErr:     true,
			errContains: "为空",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewDeployObjects(tt.deployCfg, tt.serviceConfigs, tt.envName, map[string]string{
				"//some/path:service_image":  "registry.example.com/team/service@sha256:1111111111111111111111111111111111111111111111111111111111111111",
				"//some/path:service1_image": "registry.example.com/team/service1@sha256:2222222222222222222222222222222222222222222222222222222222222222",
				"//some/path:service2_image": "registry.example.com/team/service2@sha256:3333333333333333333333333333333333333333333333333333333333333333",
			})
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewDeployObjects() expected error")
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error message should contain '%s', got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("NewDeployObjects() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestNewDeployObjects_UsesInjectedResolvedImages(t *testing.T) {
	deployCfg := &config.DeployConfig{
		App:      "grpc-hello-world",
		Template: "deploy",
		Desc:     "dev",
		Services: []*config.DeployService{
			{
				Artifact: config.DeployArtifact{Path: "//svc/service.yaml", Name: "service"},
			},
		},
	}

	serviceConfigs := []*config.ServiceConfig{
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
	}

	objects, err := NewDeployObjects(deployCfg, serviceConfigs, "dev", map[string]string{
		"//svc:service_image": "registry.example.com/team/service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatalf("NewDeployObjects() failed: %v", err)
	}

	if len(objects.Deployments) != 1 {
		t.Fatalf("deployment count = %d, want 1", len(objects.Deployments))
	}
	if objects.Deployments[0].Image != "registry.example.com/team/service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("deployment image = %q, want injected resolved image", objects.Deployments[0].Image)
	}
}

func TestNewDeployObjects_ErrorsWhenResolvedImageMissing(t *testing.T) {
	deployCfg := &config.DeployConfig{
		App:      "grpc-hello-world",
		Template: "deploy",
		Desc:     "dev",
		Services: []*config.DeployService{
			{
				Artifact: config.DeployArtifact{Path: "//svc/service.yaml", Name: "service"},
			},
		},
	}

	serviceConfigs := []*config.ServiceConfig{
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
	}

	_, err := NewDeployObjects(deployCfg, serviceConfigs, "dev", map[string]string{})
	if err == nil {
		t.Fatal("NewDeployObjects() succeeded unexpectedly")
	}
	wantErr := "artifact target //svc:service_image missing resolved image"
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("NewDeployObjects() err = %v, want substring %q", err, wantErr)
	}
}
