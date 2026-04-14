package k8s

import (
	"testing"

	"dominion/projects/infra/deploy/domain"
)

// newTestEnv 创建一个用于测试的 Environment。
func newTestEnv(t *testing.T, desiredState *domain.DesiredState) *domain.Environment {
	t.Helper()
	envName, err := domain.NewEnvironmentName("tstscope", "dev")
	if err != nil {
		t.Fatalf("创建环境名失败: %v", err)
	}
	env, err := domain.NewEnvironment(envName, "test environment", desiredState)
	if err != nil {
		t.Fatalf("创建环境失败: %v", err)
	}
	return env
}

// newTestConfig 创建一个用于测试的 K8sConfig。
func newTestConfig() *K8sConfig {
	return &K8sConfig{
		Namespace: "test-ns",
		ManagedBy: "deploy",
		Gateway: GatewayConfig{
			Name:      "test-gateway",
			Namespace: "test-gw-ns",
		},
	}
}

func TestConvertToWorkloads(t *testing.T) {
	tests := []struct {
		name     string
		env      *domain.Environment
		cfg      *K8sConfig
		wantErr  bool
		validate func(t *testing.T, objects *DeployObjects)
	}{
		{
			name: "services only",
			env: newTestEnv(t, &domain.DesiredState{
				Services: []*domain.ServiceSpec{
					{
						Name:     "web",
						App:      "webapp",
						Image:    "webapp:v1",
						Ports:    []domain.ServicePortSpec{{Name: "http", Port: 8080}},
						Replicas: 3,
					},
				},
			}),
			cfg:     newTestConfig(),
			wantErr: false,
			validate: func(t *testing.T, objects *DeployObjects) {
				t.Helper()
				if len(objects.Deployments) != 1 {
					t.Fatalf("Deployments 长度期望 1, 实际 %d", len(objects.Deployments))
				}
				d := objects.Deployments[0]
				if d.ServiceName != "web" {
					t.Errorf("ServiceName 期望 web, 实际 %s", d.ServiceName)
				}
				if d.App != "webapp" {
					t.Errorf("App 期望 webapp, 实际 %s", d.App)
				}
				if d.Image != "webapp:v1" {
					t.Errorf("Image 期望 webapp:v1, 实际 %s", d.Image)
				}
				if d.Replicas != 3 {
					t.Errorf("Replicas 期望 3, 实际 %d", d.Replicas)
				}
				if d.TLSEnabled {
					t.Error("TLSEnabled 期望 false")
				}
				if len(d.Ports) != 1 {
					t.Fatalf("Ports 长度期望 1, 实际 %d", len(d.Ports))
				}
				if d.Ports[0].Name != "http" {
					t.Errorf("Port Name 期望 http, 实际 %s", d.Ports[0].Name)
				}
				if d.Ports[0].Port != 8080 {
					t.Errorf("Port 期望 8080, 实际 %d", d.Ports[0].Port)
				}
				if d.EnvironmentName != "deploy/scopes/tstscope/environments/dev" {
					t.Errorf("EnvironmentName 期望完整路径, 实际 %s", d.EnvironmentName)
				}
				if objects.HTTPRoutes != nil {
					t.Errorf("HTTPRoutes 期望 nil, 实际 %v", objects.HTTPRoutes)
				}
				if objects.MongoDBWorkloads != nil {
					t.Errorf("MongoDBWorkloads 期望 nil, 实际 %v", objects.MongoDBWorkloads)
				}
			},
		},
		{
			name: "service with TLS enabled",
			env: newTestEnv(t, &domain.DesiredState{
				Services: []*domain.ServiceSpec{
					{
						Name:       "secure-svc",
						App:        "secureapp",
						Image:      "secureapp:v1",
						Ports:      []domain.ServicePortSpec{{Name: "https", Port: 8443}},
						Replicas:   1,
						TLSEnabled: true,
					},
				},
			}),
			cfg:     newTestConfig(),
			wantErr: false,
			validate: func(t *testing.T, objects *DeployObjects) {
				t.Helper()
				if !objects.Deployments[0].TLSEnabled {
					t.Error("TLSEnabled 期望 true")
				}
			},
		},
		{
			name: "mongodb infra",
			env: newTestEnv(t, &domain.DesiredState{
				Services: []*domain.ServiceSpec{
					{
						Name:     "svc1",
						App:      "app1",
						Image:    "app1:v1",
						Ports:    []domain.ServicePortSpec{{Name: "http", Port: 8080}},
						Replicas: 1,
					},
				},
				Infras: []*domain.InfraSpec{
					{
						Resource:           "mongodb",
						Profile:            "dev-single",
						Name:               "mongo1",
						App:                "myapp",
						PersistenceEnabled: true,
					},
				},
			}),
			cfg:     newTestConfig(),
			wantErr: false,
			validate: func(t *testing.T, objects *DeployObjects) {
				t.Helper()
				if len(objects.MongoDBWorkloads) != 1 {
					t.Fatalf("MongoDBWorkloads 长度期望 1, 实际 %d", len(objects.MongoDBWorkloads))
				}
				m := objects.MongoDBWorkloads[0]
				if m.ServiceName != "mongo1" {
					t.Errorf("ServiceName 期望 mongo1, 实际 %s", m.ServiceName)
				}
				if m.App != "myapp" {
					t.Errorf("App 期望 myapp, 实际 %s", m.App)
				}
				if m.ProfileName != "dev-single" {
					t.Errorf("ProfileName 期望 dev-single, 实际 %s", m.ProfileName)
				}
				if !m.Persistence.Enabled {
					t.Error("Persistence.Enabled 期望 true")
				}
				if m.EnvironmentName != "deploy/scopes/tstscope/environments/dev" {
					t.Errorf("EnvironmentName 期望完整路径, 实际 %s", m.EnvironmentName)
				}
			},
		},
		{
			name: "mongodb without persistence",
			env: newTestEnv(t, &domain.DesiredState{
				Services: []*domain.ServiceSpec{
					{
						Name:     "svc1",
						App:      "app1",
						Image:    "app1:v1",
						Ports:    []domain.ServicePortSpec{{Name: "http", Port: 8080}},
						Replicas: 1,
					},
				},
				Infras: []*domain.InfraSpec{
					{
						Resource: "mongodb",
						Profile:  "dev-single",
						Name:     "cache",
						App:      "myapp",
					},
				},
			}),
			cfg:     newTestConfig(),
			wantErr: false,
			validate: func(t *testing.T, objects *DeployObjects) {
				t.Helper()
				if objects.MongoDBWorkloads[0].Persistence.Enabled {
					t.Error("Persistence.Enabled 期望 false")
				}
			},
		},
		{
			name: "unknown infra resource returns error",
			env: newTestEnv(t, &domain.DesiredState{
				Services: []*domain.ServiceSpec{
					{
						Name:     "svc1",
						App:      "app1",
						Image:    "app1:v1",
						Ports:    []domain.ServicePortSpec{{Name: "http", Port: 8080}},
						Replicas: 1,
					},
				},
				Infras: []*domain.InfraSpec{
					{
						Resource: "redis",
						Name:     "cache",
						App:      "myapp",
					},
				},
			}),
			cfg:     newTestConfig(),
			wantErr: true,
		},
		{
			name: "http route with single rule",
			env: newTestEnv(t, &domain.DesiredState{
				Services: []*domain.ServiceSpec{
					{
						Name:     "api",
						App:      "apiapp",
						Image:    "apiapp:v1",
						Ports:    []domain.ServicePortSpec{{Name: "http", Port: 9090}},
						Replicas: 2,
					},
				},
				HTTPRoutes: []*domain.HTTPRouteSpec{
					{
						Hostnames: []string{"api.example.com"},
						Rules: []domain.HTTPRouteRule{
							{
								Backend: "api",
								Path: domain.HTTPPathRule{
									Type:  domain.HTTPPathRuleTypePathPrefix,
									Value: "/v1",
								},
							},
						},
					},
				},
			}),
			cfg:     newTestConfig(),
			wantErr: false,
			validate: func(t *testing.T, objects *DeployObjects) {
				t.Helper()
				if len(objects.HTTPRoutes) != 1 {
					t.Fatalf("HTTPRoutes 长度期望 1, 实际 %d", len(objects.HTTPRoutes))
				}
				r := objects.HTTPRoutes[0]
				if r.ServiceName != "api" {
					t.Errorf("ServiceName 期望 api, 实际 %s", r.ServiceName)
				}
				if r.App != "apiapp" {
					t.Errorf("App 期望 apiapp, 实际 %s", r.App)
				}
				if len(r.Hostnames) != 1 || r.Hostnames[0] != "api.example.com" {
					t.Errorf("Hostnames 期望 [api.example.com], 实际 %v", r.Hostnames)
				}
				if r.GatewayName != "test-gateway" {
					t.Errorf("GatewayName 期望 test-gateway, 实际 %s", r.GatewayName)
				}
				if r.GatewayNamespace != "test-gw-ns" {
					t.Errorf("GatewayNamespace 期望 test-gw-ns, 实际 %s", r.GatewayNamespace)
				}
				if len(r.Matches) != 1 {
					t.Fatalf("Matches 长度期望 1, 实际 %d", len(r.Matches))
				}
				m := r.Matches[0]
				if m.Type != HTTPPathMatchTypePathPrefix {
					t.Errorf("Match Type 期望 HTTPPathMatchTypePathPrefix, 实际 %v", m.Type)
				}
				if m.Value != "/v1" {
					t.Errorf("Match Value 期望 /v1, 实际 %s", m.Value)
				}
				if m.BackendPort != 9090 {
					t.Errorf("Match BackendPort 期望 9090, 实际 %d", m.BackendPort)
				}
				// BackendService 应为 deployment 的 ServiceResourceName
				expectedBackend := objects.Deployments[0].ServiceResourceName()
				if r.BackendService != expectedBackend {
					t.Errorf("BackendService 期望 %s, 实际 %s", expectedBackend, r.BackendService)
				}
				if m.BackendName != expectedBackend {
					t.Errorf("BackendName 期望 %s, 实际 %s", expectedBackend, m.BackendName)
				}
			},
		},
		{
			name: "http route with multiple rules",
			env: newTestEnv(t, &domain.DesiredState{
				Services: []*domain.ServiceSpec{
					{
						Name:     "svc1",
						App:      "app1",
						Image:    "app1:v1",
						Ports:    []domain.ServicePortSpec{{Name: "http", Port: 8080}},
						Replicas: 1,
					},
					{
						Name:     "svc2",
						App:      "app2",
						Image:    "app2:v1",
						Ports:    []domain.ServicePortSpec{{Name: "grpc", Port: 50051}},
						Replicas: 1,
					},
				},
				HTTPRoutes: []*domain.HTTPRouteSpec{
					{
						Hostnames: []string{"multi.example.com"},
						Rules: []domain.HTTPRouteRule{
							{
								Backend: "svc1",
								Path: domain.HTTPPathRule{
									Type:  domain.HTTPPathRuleTypePathPrefix,
									Value: "/api",
								},
							},
							{
								Backend: "svc2",
								Path: domain.HTTPPathRule{
									Type:  domain.HTTPPathRuleTypePathPrefix,
									Value: "/grpc",
								},
							},
						},
					},
				},
			}),
			cfg:     newTestConfig(),
			wantErr: false,
			validate: func(t *testing.T, objects *DeployObjects) {
				t.Helper()
				if len(objects.HTTPRoutes) != 1 {
					t.Fatalf("HTTPRoutes 长度期望 1, 实际 %d", len(objects.HTTPRoutes))
				}
				r := objects.HTTPRoutes[0]
				// 首条规则的 backend 作为主后端
				if r.ServiceName != "svc1" {
					t.Errorf("ServiceName 期望 svc1, 实际 %s", r.ServiceName)
				}
				if len(r.Matches) != 2 {
					t.Fatalf("Matches 长度期望 2, 实际 %d", len(r.Matches))
				}
				// 第一条 match 指向 svc1
				if r.Matches[0].BackendPort != 8080 {
					t.Errorf("Match[0] BackendPort 期望 8080, 实际 %d", r.Matches[0].BackendPort)
				}
				if r.Matches[0].BackendName != objects.Deployments[0].ServiceResourceName() {
					t.Errorf("Match[0] BackendName 期望 %s, 实际 %s",
						objects.Deployments[0].ServiceResourceName(), r.Matches[0].BackendName)
				}
				// 第二条 match 指向 svc2
				if r.Matches[1].BackendPort != 50051 {
					t.Errorf("Match[1] BackendPort 期望 50051, 实际 %d", r.Matches[1].BackendPort)
				}
				if r.Matches[1].BackendName != objects.Deployments[1].ServiceResourceName() {
					t.Errorf("Match[1] BackendName 期望 %s, 实际 %s",
						objects.Deployments[1].ServiceResourceName(), r.Matches[1].BackendName)
				}
			},
		},
		{
			name: "full environment with all workload types",
			env: newTestEnv(t, &domain.DesiredState{
				Services: []*domain.ServiceSpec{
					{
						Name:     "web",
						App:      "webapp",
						Image:    "webapp:v1",
						Ports:    []domain.ServicePortSpec{{Name: "http", Port: 8080}},
						Replicas: 2,
					},
					{
						Name:     "api",
						App:      "apiapp",
						Image:    "apiapp:v2",
						Ports:    []domain.ServicePortSpec{{Name: "grpc", Port: 50051}},
						Replicas: 1,
					},
				},
				Infras: []*domain.InfraSpec{
					{
						Resource:           "mongodb",
						Profile:            "dev-single",
						Name:               "mongo",
						App:                "webapp",
						PersistenceEnabled: true,
					},
				},
				HTTPRoutes: []*domain.HTTPRouteSpec{
					{
						Hostnames: []string{"web.example.com"},
						Rules: []domain.HTTPRouteRule{
							{
								Backend: "web",
								Path: domain.HTTPPathRule{
									Type:  domain.HTTPPathRuleTypePathPrefix,
									Value: "/",
								},
							},
						},
					},
				},
			}),
			cfg:     newTestConfig(),
			wantErr: false,
			validate: func(t *testing.T, objects *DeployObjects) {
				t.Helper()
				if len(objects.Deployments) != 2 {
					t.Errorf("Deployments 长度期望 2, 实际 %d", len(objects.Deployments))
				}
				if len(objects.MongoDBWorkloads) != 1 {
					t.Errorf("MongoDBWorkloads 长度期望 1, 实际 %d", len(objects.MongoDBWorkloads))
				}
				if len(objects.HTTPRoutes) != 1 {
					t.Errorf("HTTPRoutes 长度期望 1, 实际 %d", len(objects.HTTPRoutes))
				}
			},
		},
		{
			name: "empty desired state",
			env: newTestEnv(t, &domain.DesiredState{
				Services:   []*domain.ServiceSpec{},
				Infras:     []*domain.InfraSpec{},
				HTTPRoutes: []*domain.HTTPRouteSpec{},
			}),
			cfg:     newTestConfig(),
			wantErr: false,
			validate: func(t *testing.T, objects *DeployObjects) {
				t.Helper()
				if objects.Deployments != nil {
					t.Errorf("Deployments 期望 nil, 实际 %v", objects.Deployments)
				}
				if objects.MongoDBWorkloads != nil {
					t.Errorf("MongoDBWorkloads 期望 nil, 实际 %v", objects.MongoDBWorkloads)
				}
				if objects.HTTPRoutes != nil {
					t.Errorf("HTTPRoutes 期望 nil, 实际 %v", objects.HTTPRoutes)
				}
			},
		},
		{
			name: "service without ports",
			env: newTestEnv(t, &domain.DesiredState{
				Services: []*domain.ServiceSpec{
					{
						Name:     "worker",
						App:      "workerapp",
						Image:    "workerapp:v1",
						Replicas: 1,
					},
				},
			}),
			cfg:     newTestConfig(),
			wantErr: false,
			validate: func(t *testing.T, objects *DeployObjects) {
				t.Helper()
				if objects.Deployments[0].Ports != nil {
					t.Errorf("Ports 期望 nil, 实际 %v", objects.Deployments[0].Ports)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertToWorkloads(tt.env, tt.cfg)
			if tt.wantErr && err == nil {
				t.Fatalf("期望返回错误, 实际返回 nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("不期望返回错误, 实际返回: %v", err)
			}
			if tt.wantErr {
				return
			}
			if tt.validate != nil {
				tt.validate(t, got)
			}
		})
	}
}

func Test_convertPorts(t *testing.T) {
	tests := []struct {
		name  string
		ports []domain.ServicePortSpec
		want  []*DeploymentPort
	}{
		{
			name:  "nil ports returns nil",
			ports: nil,
			want:  nil,
		},
		{
			name:  "empty ports returns nil",
			ports: []domain.ServicePortSpec{},
			want:  nil,
		},
		{
			name: "single port",
			ports: []domain.ServicePortSpec{
				{Name: "http", Port: 8080},
			},
			want: []*DeploymentPort{
				{Name: "http", Port: 8080},
			},
		},
		{
			name: "multiple ports",
			ports: []domain.ServicePortSpec{
				{Name: "http", Port: 8080},
				{Name: "grpc", Port: 50051},
			},
			want: []*DeploymentPort{
				{Name: "http", Port: 8080},
				{Name: "grpc", Port: 50051},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertPorts(tt.ports)
			if len(got) != len(tt.want) {
				t.Fatalf("长度期望 %d, 实际 %d", len(tt.want), len(got))
			}
			for i, p := range got {
				if p.Name != tt.want[i].Name {
					t.Errorf("ports[%d].Name 期望 %s, 实际 %s", i, tt.want[i].Name, p.Name)
				}
				if p.Port != tt.want[i].Port {
					t.Errorf("ports[%d].Port 期望 %d, 实际 %d", i, tt.want[i].Port, p.Port)
				}
			}
		})
	}
}

func Test_convertPathType(t *testing.T) {
	tests := []struct {
		name  string
		input domain.HTTPPathRuleType
		want  HTTPPathMatchType
	}{
		{
			name:  "unspecified maps to unspecified",
			input: domain.HTTPPathRuleTypeUnspecified,
			want:  HTTPPathMatchTypeUnspecified,
		},
		{
			name:  "path prefix maps to path prefix",
			input: domain.HTTPPathRuleTypePathPrefix,
			want:  HTTPPathMatchTypePathPrefix,
		},
		{
			name:  "unknown value maps to unspecified",
			input: domain.HTTPPathRuleType(99),
			want:  HTTPPathMatchTypeUnspecified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertPathType(tt.input)
			if got != tt.want {
				t.Errorf("convertPathType(%v) 期望 %v, 实际 %v", tt.input, tt.want, got)
			}
		})
	}
}
