package k8s

import (
	"encoding/base64"
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

	objects, err := NewDeployObjects(deployCfg, serviceConfigs, "dev", "grpc-hello-world", map[string]string{
		"//some/path:service_image": "registry.example.com/team/service@sha256:1111111111111111111111111111111111111111111111111111111111111111",
	})
	if err != nil {
		t.Fatalf("NewDeployObjects() failed: %v", err)
	}

	if len(objects.Deployments) != 1 || len(objects.HTTPRoutes) != 0 {
		t.Fatalf("unexpected object counts: deployments=%d routes=%d", len(objects.Deployments), len(objects.HTTPRoutes))
	}
	if objects.Deployments[0].EnvironmentName != "dev" {
		t.Fatal("environment name was not propagated into deployment workload")
	}
	if objects.Deployments[0].Image != "registry.example.com/team/service@sha256:1111111111111111111111111111111111111111111111111111111111111111" {
		t.Fatalf("deployment image = %q, want injected resolved image", objects.Deployments[0].Image)
	}
	if objects.Deployments[0].DominionApp != "grpc-hello-world" {
		t.Fatalf("deployment dominion app = %q, want %q", objects.Deployments[0].DominionApp, "grpc-hello-world")
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

	objects, err := NewDeployObjects(deployCfg, serviceConfigs, "dev", "grpc-hello-world", map[string]string{
		"//some/path:service_image": "registry.example.com/team/service@sha256:1111111111111111111111111111111111111111111111111111111111111111",
		"//some/path:gateway_image": "registry.example.com/team/gateway@sha256:2222222222222222222222222222222222222222222222222222222222222222",
	})
	if err != nil {
		t.Fatalf("NewDeployObjects() failed: %v", err)
	}

	if len(objects.Deployments) != 2 || len(objects.HTTPRoutes) != 1 {
		t.Fatalf("unexpected object counts: deployments=%d routes=%d", len(objects.Deployments), len(objects.HTTPRoutes))
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

	objects, err := NewDeployObjects(deployCfg, serviceConfigs, "dev", "grpc-hello-world", map[string]string{
		"//some/path:service_image": "registry.example.com/team/service@sha256:1111111111111111111111111111111111111111111111111111111111111111",
		"//some/path:gateway_image": "registry.example.com/team/gateway@sha256:2222222222222222222222222222222222222222222222222222222222222222",
	})
	if err != nil {
		t.Fatalf("NewDeployObjects() failed: %v", err)
	}

	if len(objects.Deployments) != 2 {
		t.Fatalf("unexpected object counts: deployments=%d", len(objects.Deployments))
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
			_, err := NewDeployObjects(tt.deployCfg, tt.serviceConfigs, tt.envName, "grpc-hello-world", map[string]string{
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

	objects, err := NewDeployObjects(deployCfg, serviceConfigs, "dev", "grpc-hello-world", map[string]string{
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

func TestNewDeployObjects_TLSEnabled(t *testing.T) {
	tests := []struct {
		name        string
		artifactTLS bool
		wantEnabled bool
	}{
		{
			name:        "tls enabled marks deployment workload",
			artifactTLS: true,
			wantEnabled: true,
		},
		{
			name:        "tls disabled keeps deployment workload plain",
			artifactTLS: false,
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployCfg := &config.DeployConfig{
				App:      "grpc-hello-world",
				Template: "deploy",
				Desc:     "dev",
				Services: []*config.DeployService{{
					Artifact: config.DeployArtifact{Path: "//svc/service.yaml", Name: "service"},
				}},
			}

			serviceConfigs := []*config.ServiceConfig{{
				URI:  "//svc/service.yaml",
				Name: "service",
				App:  "grpc-hello-world",
				Desc: "grpc service",
				Artifacts: []*config.ServiceArtifact{{
					Name:   "service",
					Type:   config.ServiceArtifactTypeDeployment,
					Target: "//svc:service_image",
					TLS:    tt.artifactTLS,
					Ports:  []*config.ServiceArtifactPort{{Name: "grpc", Port: 50051}},
				}},
			}}

			objects, err := NewDeployObjects(deployCfg, serviceConfigs, "dev", "grpc-hello-world", map[string]string{
				"//svc:service_image": "registry.example.com/team/service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			})

			if err != nil {
				t.Fatalf("NewDeployObjects() failed: %v", err)
			}
			if len(objects.Deployments) != 1 {
				t.Fatalf("deployment count = %d, want 1", len(objects.Deployments))
			}

			deployment := objects.Deployments[0]
			if deployment.TLSEnabled != tt.wantEnabled {
				t.Fatalf("deployment tls enabled = %t, want %t", deployment.TLSEnabled, tt.wantEnabled)
			}
		})
	}
}

func TestNewDeployObjects_DominionAppMismatchRegression(t *testing.T) {
	deployCfg := &config.DeployConfig{
		App:      "grpc-hello-world",
		Template: "deploy",
		Desc:     "dev",
		Services: []*config.DeployService{{
			Artifact: config.DeployArtifact{Path: "//svc/service.yaml", Name: "gateway"},
		}},
	}

	serviceConfigs := []*config.ServiceConfig{{
		URI:  "//svc/service.yaml",
		Name: "gateway",
		App:  "grpc_hello_world",
		Desc: "gateway service",
		Artifacts: []*config.ServiceArtifact{{
			Name:   "gateway",
			Type:   config.ServiceArtifactTypeDeployment,
			Target: "//some/path:gateway_image",
			Ports:  []*config.ServiceArtifactPort{{Name: "grpc", Port: 50051}},
		}},
	}}

	objects, err := NewDeployObjects(deployCfg, serviceConfigs, "dev", "grpc-hello-world", map[string]string{
		"//some/path:gateway_image": "registry.example.com/team/gateway@sha256:2222222222222222222222222222222222222222222222222222222222222222",
	})
	if err != nil {
		t.Fatalf("NewDeployObjects() failed: %v", err)
	}
	if len(objects.Deployments) != 1 {
		t.Fatalf("unexpected object counts: deployments=%d", len(objects.Deployments))
	}

	deployment := objects.Deployments[0]
	if deployment.App != "grpc_hello_world" {
		t.Fatalf("deployment app = %q, want %q", deployment.App, "grpc_hello_world")
	}
	if deployment.DominionApp != "grpc-hello-world" {
		t.Fatalf("deployment dominion app = %q, want %q", deployment.DominionApp, "grpc-hello-world")
	}

	k8sConfig := newTestK8sConfig()
	stubLoadK8sConfig(t, k8sConfig)

	builtDeployment, err := BuildDeployment(deployment)
	if err != nil {
		t.Fatalf("BuildDeployment() failed: %v", err)
	}
	if builtDeployment.Labels[appLabelKey] != "grpc_hello_world" {
		t.Fatalf("deployment app label = %q, want %q", builtDeployment.Labels[appLabelKey], "grpc_hello_world")
	}
	if builtDeployment.Labels[dominionAppLabelKey] != "grpc-hello-world" {
		t.Fatalf("deployment dominion app label = %q, want %q", builtDeployment.Labels[dominionAppLabelKey], "grpc-hello-world")
	}
	if len(builtDeployment.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("deployment containers len = %d, want 1", len(builtDeployment.Spec.Template.Spec.Containers))
	}
	gotEnv := builtDeployment.Spec.Template.Spec.Containers[0].Env
	if len(gotEnv) != 3 {
		t.Fatalf("deployment env len = %d, want 3", len(gotEnv))
	}
	if gotEnv[0].Name != reservedEnvNameDominionApp || gotEnv[0].Value != "grpc-hello-world" {
		t.Fatalf("deployment env[0] = %#v, want DOMINION_APP literal", gotEnv[0])
	}
	if gotEnv[1].Name != reservedEnvNameDominionEnvironment || gotEnv[1].Value != "dev" {
		t.Fatalf("deployment env[1] = %#v, want DOMINION_ENVIRONMENT literal", gotEnv[1])
	}
	if gotEnv[2].Name != reservedEnvNamePodNamespace || gotEnv[2].Value != k8sConfig.Namespace {
		t.Fatalf("deployment env[2] = %#v, want POD_NAMESPACE literal", gotEnv[2])
	}

	builtService, err := BuildService(deployment)
	if err != nil {
		t.Fatalf("BuildService() failed: %v", err)
	}
	if builtService.Labels[appLabelKey] != "grpc_hello_world" {
		t.Fatalf("service app label = %q, want %q", builtService.Labels[appLabelKey], "grpc_hello_world")
	}
	if builtService.Labels[dominionAppLabelKey] != "grpc-hello-world" {
		t.Fatalf("service dominion app label = %q, want %q", builtService.Labels[dominionAppLabelKey], "grpc-hello-world")
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

	_, err := NewDeployObjects(deployCfg, serviceConfigs, "dev", "grpc-hello-world", map[string]string{})
	if err == nil {
		t.Fatal("NewDeployObjects() succeeded unexpectedly")
	}
	wantErr := "artifact target //svc:service_image missing resolved image"
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("NewDeployObjects() err = %v, want substring %q", err, wantErr)
	}
}

func TestNewDeployObjects_InfraMongoDB(t *testing.T) {
	tests := []struct {
		name               string
		persistenceEnabled bool
		wantPVCCount       int
	}{
		{
			name:               "persistence enabled creates mongodb workloads including pvc",
			persistenceEnabled: true,
			wantPVCCount:       1,
		},
		{
			name:               "persistence disabled skips pvc workload",
			persistenceEnabled: false,
			wantPVCCount:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8sConfig := newTestK8sConfigWithMongoProfile()
			stubLoadK8sConfig(t, k8sConfig)

			deployCfg := &config.DeployConfig{
				App:      "grpc-hello-world",
				Template: "deploy",
				Desc:     "dev",
				Services: []*config.DeployService{{
					Infra: config.DeployInfra{
						Resource: "mongodb",
						Profile:  "dev-single",
						Name:     "mongo-main",
						Persistence: config.DeployInfraPersistence{
							Enabled: tt.persistenceEnabled,
						},
					},
				}},
			}

			objects, err := NewDeployObjects(deployCfg, nil, "dev", "grpc-hello-world", nil)
			if err != nil {
				t.Fatalf("NewDeployObjects() failed: %v", err)
			}

			if len(objects.Deployments) != 0 {
				t.Fatalf("deployment count = %d, want 0", len(objects.Deployments))
			}
			if len(objects.HTTPRoutes) != 0 {
				t.Fatalf("http route count = %d, want 0", len(objects.HTTPRoutes))
			}
			if len(objects.MongoDBWorkloads) != 1 {
				t.Fatalf("mongodb workload count = %d, want 1", len(objects.MongoDBWorkloads))
			}

			mongoWorkload := objects.MongoDBWorkloads[0]
			if mongoWorkload.ServiceName != "mongo-main" {
				t.Fatalf("mongodb workload service name = %q, want %q", mongoWorkload.ServiceName, "mongo-main")
			}
			if mongoWorkload.EnvironmentName != "dev" {
				t.Fatalf("mongodb workload environment = %q, want %q", mongoWorkload.EnvironmentName, "dev")
			}
			if mongoWorkload.ProfileName != "dev-single" {
				t.Fatalf("mongodb workload profile = %q, want %q", mongoWorkload.ProfileName, "dev-single")
			}
			if mongoWorkload.Persistence.Enabled != tt.persistenceEnabled {
				t.Fatalf("mongodb workload persistence enabled = %t, want %t", mongoWorkload.Persistence.Enabled, tt.persistenceEnabled)
			}
			gotPVCCount := 0
			if mongoWorkload.Persistence.Enabled {
				gotPVCCount = 1
			}
			if gotPVCCount != tt.wantPVCCount {
				t.Fatalf("mongodb pvc workload count = %d, want %d", gotPVCCount, tt.wantPVCCount)
			}

			wantDeploymentName := newObjectName(WorkloadKindMongoDB, "grpc-hello-world", "grpc-hello-world", "mongo-main", "dev")
			if mongoWorkload.ResourceName() != wantDeploymentName {
				t.Fatalf("mongodb workload resource name = %q, want %q", mongoWorkload.ResourceName(), wantDeploymentName)
			}

			deployment, err := BuildMongoDBDeployment(mongoWorkload)
			if err != nil {
				t.Fatalf("BuildMongoDBDeployment() failed: %v", err)
			}
			if len(deployment.Spec.Template.Spec.Containers) != 1 {
				t.Fatalf("mongodb deployment containers len = %d, want 1", len(deployment.Spec.Template.Spec.Containers))
			}
			if deployment.Spec.Template.Spec.Containers[0].Image != "mongo:7.0" {
				t.Fatalf("mongodb deployment image = %q, want %q", deployment.Spec.Template.Spec.Containers[0].Image, "mongo:7.0")
			}
			if deployment.Labels[appLabelKey] != "grpc-hello-world" {
				t.Fatalf("mongodb deployment app label = %q, want %q", deployment.Labels[appLabelKey], "grpc-hello-world")
			}

			service, err := BuildMongoDBService(mongoWorkload)
			if err != nil {
				t.Fatalf("BuildMongoDBService() failed: %v", err)
			}
			wantServiceName := newObjectName(WorkloadKindService, "grpc-hello-world", "grpc-hello-world", "mongo-main", "dev")
			if service.Name != wantServiceName {
				t.Fatalf("mongodb service name = %q, want %q", service.Name, wantServiceName)
			}
			if len(service.Spec.Ports) != 1 {
				t.Fatalf("mongodb service port count = %d, want 1", len(service.Spec.Ports))
			}
			if service.Spec.Ports[0].Port != 27017 {
				t.Fatalf("mongodb service port = %d, want 27017", service.Spec.Ports[0].Port)
			}
			if service.Labels[dominionAppLabelKey] != "grpc-hello-world" {
				t.Fatalf("mongodb service dominion app label = %q, want %q", service.Labels[dominionAppLabelKey], "grpc-hello-world")
			}

			secret, err := BuildMongoDBSecret(mongoWorkload)
			if err != nil {
				t.Fatalf("BuildMongoDBSecret() failed: %v", err)
			}
			wantSecretName := newObjectName(WorkloadKindSecret, "grpc-hello-world", "grpc-hello-world", "mongo-main", "dev")
			if secret.Name != wantSecretName {
				t.Fatalf("mongodb secret name = %q, want %q", secret.Name, wantSecretName)
			}
			username, err := base64.StdEncoding.DecodeString(string(secret.Data[mongoSecretUsernameKey]))
			if err != nil {
				t.Fatalf("DecodeString(username) failed: %v", err)
			}
			if string(username) != "admin" {
				t.Fatalf("mongodb secret username = %q, want %q", string(username), "admin")
			}
		})
	}
}

func TestNewDeployObjects_InfraAndArtifactMutuallyExclusive(t *testing.T) {
	k8sConfig := newTestK8sConfigWithMongoProfile()
	stubLoadK8sConfig(t, k8sConfig)

	deployCfg := &config.DeployConfig{
		App:      "grpc-hello-world",
		Template: "deploy",
		Desc:     "dev",
		Services: []*config.DeployService{{
			Artifact: config.DeployArtifact{Path: "//svc/service.yaml", Name: "service"},
			Infra: config.DeployInfra{
				Resource: "mongodb",
				Profile:  "dev-single",
				Name:     "mongo-main",
			},
		}},
	}

	_, err := NewDeployObjects(deployCfg, nil, "dev", "grpc-hello-world", nil)
	if err == nil {
		t.Fatal("NewDeployObjects() succeeded unexpectedly")
	}
	if !strings.Contains(err.Error(), "infra 和 artifact 不能同时配置") {
		t.Fatalf("NewDeployObjects() err = %v, want mutual exclusivity error", err)
	}
}

func TestNewDeployObjects_InfraMongoDBPasswordDeterministic(t *testing.T) {
	k8sConfig := newTestK8sConfigWithMongoProfile()
	stubLoadK8sConfig(t, k8sConfig)

	deployCfg := &config.DeployConfig{
		App:      "grpc-hello-world",
		Template: "deploy",
		Desc:     "dev",
		Services: []*config.DeployService{{
			Infra: config.DeployInfra{
				Resource: "mongodb",
				Profile:  "dev-single",
				Name:     "mongo-main",
				Persistence: config.DeployInfraPersistence{
					Enabled: true,
				},
			},
		}},
	}

	objectsA, err := NewDeployObjects(deployCfg, nil, "dev", "grpc-hello-world", nil)
	if err != nil {
		t.Fatalf("NewDeployObjects() first call failed: %v", err)
	}
	objectsB, err := NewDeployObjects(deployCfg, nil, "dev", "grpc-hello-world", nil)
	if err != nil {
		t.Fatalf("NewDeployObjects() second call failed: %v", err)
	}

	secretA, err := BuildMongoDBSecret(objectsA.MongoDBWorkloads[0])
	if err != nil {
		t.Fatalf("BuildMongoDBSecret() first call failed: %v", err)
	}
	secretB, err := BuildMongoDBSecret(objectsB.MongoDBWorkloads[0])
	if err != nil {
		t.Fatalf("BuildMongoDBSecret() second call failed: %v", err)
	}

	passwordA, err := base64.StdEncoding.DecodeString(string(secretA.Data[mongoSecretPasswordKey]))
	if err != nil {
		t.Fatalf("DecodeString(passwordA) failed: %v", err)
	}
	passwordB, err := base64.StdEncoding.DecodeString(string(secretB.Data[mongoSecretPasswordKey]))
	if err != nil {
		t.Fatalf("DecodeString(passwordB) failed: %v", err)
	}

	wantPassword := generateStablePassword("grpc-hello-world", "dev", "mongo-main")
	if string(passwordA) != wantPassword {
		t.Fatalf("mongodb password A = %q, want %q", string(passwordA), wantPassword)
	}
	if string(passwordB) != wantPassword {
		t.Fatalf("mongodb password B = %q, want %q", string(passwordB), wantPassword)
	}
}
