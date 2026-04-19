package k8s

import (
	"fmt"
	"reflect"
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
	env, err := domain.NewEnvironment(envName, domain.EnvironmentTypeProd, "test environment", desiredState)
	if err != nil {
		t.Fatalf("创建环境失败: %v", err)
	}
	return env
}

func newTestEnvWithType(t *testing.T, envType domain.EnvironmentType, desiredState *domain.DesiredState) *domain.Environment {
	t.Helper()
	envName, err := domain.NewEnvironmentName("tstscope", "dev")
	if err != nil {
		t.Fatalf("创建环境名失败: %v", err)
	}
	env, err := domain.NewEnvironment(envName, envType, "test environment", desiredState)
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
		name           string
		env            *domain.Environment
		cfg            *K8sConfig
		want           *DeployObjects
		wantErrMessage string
	}{
		{
			name: "services only",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					{
						Name:     "web",
						App:      "webapp",
						Image:    "webapp:v1",
						Ports:    []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
						Replicas: 3,
					},
				},
			}),
			cfg:  newTestConfig(),
			want: wantDeployObjectsWithServicesOnly(),
		},
		{
			name: "service with TLS enabled",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					{
						Name:       "secure-svc",
						App:        "secureapp",
						Image:      "secureapp:v1",
						Ports:      []domain.ArtifactPortSpec{{Name: "https", Port: 8443}},
						Replicas:   1,
						TLSEnabled: true,
					},
				},
			}),
			cfg:  newTestConfig(),
			want: wantDeployObjectsWithTLSEnabled(),
		},
		{
			name: "mongodb infra",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					{
						Name:     "svc1",
						App:      "app1",
						Image:    "app1:v1",
						Ports:    []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
						Replicas: 1,
					},
				},
				Infras: []*domain.InfraSpec{
					{
						Resource: "mongodb",
						Profile:  "dev-single",
						Name:     "mongo1",
						App:      "myapp",
						Persistence: domain.InfraPersistenceSpec{
							Enabled: true,
						},
					},
				},
			}),
			cfg:  newTestConfig(),
			want: wantDeployObjectsWithMongoDBInfra(),
		},
		{
			name: "mongodb without persistence",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					{
						Name:     "svc1",
						App:      "app1",
						Image:    "app1:v1",
						Ports:    []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
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
			cfg:  newTestConfig(),
			want: wantDeployObjectsWithMongoDBWithoutPersistence(),
		},
		{
			name: "unknown infra resource returns error",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					{
						Name:     "svc1",
						App:      "app1",
						Image:    "app1:v1",
						Ports:    []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
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
			cfg:            newTestConfig(),
			wantErrMessage: "不支持的 infra resource 类型: redis",
		},
		{
			name: "http route with single rule",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					{
						Name:     "api",
						App:      "apiapp",
						Image:    "apiapp:v1",
						Ports:    []domain.ArtifactPortSpec{{Name: "http", Port: 9090}},
						Replicas: 2,
						HTTP: &domain.ArtifactHTTPSpec{
							Hostnames: []string{"api.example.com"},
							Matches: []domain.HTTPRouteRule{
								{
									Backend: "http",
									Path: domain.HTTPPathRule{
										Type:  domain.HTTPPathRuleTypePathPrefix,
										Value: "/v1",
									},
								},
							},
						},
					},
				},
			}),
			cfg:  newTestConfig(),
			want: wantDeployObjectsWithSingleRuleHTTPRoute(),
		},
		{
			name: "http route with multiple rules",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					{
						Name:     "svc1",
						App:      "app1",
						Image:    "app1:v1",
						Ports:    []domain.ArtifactPortSpec{{Name: "http", Port: 8080}, {Name: "grpc", Port: 50051}},
						Replicas: 1,
						HTTP: &domain.ArtifactHTTPSpec{
							Hostnames: []string{"multi.example.com"},
							Matches: []domain.HTTPRouteRule{
								{
									Backend: "http",
									Path: domain.HTTPPathRule{
										Type:  domain.HTTPPathRuleTypePathPrefix,
										Value: "/api",
									},
								},
								{
									Backend: "grpc",
									Path: domain.HTTPPathRule{
										Type:  domain.HTTPPathRuleTypePathPrefix,
										Value: "/grpc",
									},
								},
							},
						},
					},
				},
			}),
			cfg:  newTestConfig(),
			want: wantDeployObjectsWithMultipleRuleHTTPRoute(),
		},
		{
			name: "full environment with all workload types",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					{
						Name:     "web",
						App:      "webapp",
						Image:    "webapp:v1",
						Ports:    []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
						Replicas: 2,
						HTTP: &domain.ArtifactHTTPSpec{
							Hostnames: []string{"web.example.com"},
							Matches: []domain.HTTPRouteRule{
								{
									Backend: "http",
									Path: domain.HTTPPathRule{
										Type:  domain.HTTPPathRuleTypePathPrefix,
										Value: "/",
									},
								},
							},
						},
					},
					{
						Name:     "api",
						App:      "apiapp",
						Image:    "apiapp:v2",
						Ports:    []domain.ArtifactPortSpec{{Name: "grpc", Port: 50051}},
						Replicas: 1,
					},
				},
				Infras: []*domain.InfraSpec{
					{
						Resource: "mongodb",
						Profile:  "dev-single",
						Name:     "mongo",
						App:      "webapp",
						Persistence: domain.InfraPersistenceSpec{
							Enabled: true,
						},
					},
				},
			}),
			cfg:  newTestConfig(),
			want: wantDeployObjectsWithAllWorkloadTypes(),
		},
		{
			name: "empty desired state",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{},
				Infras:    []*domain.InfraSpec{},
			}),
			cfg:  newTestConfig(),
			want: &DeployObjects{},
		},
		{
			name: "service without ports",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					{
						Name:     "worker",
						App:      "workerapp",
						Image:    "workerapp:v1",
						Replicas: 1,
					},
				},
			}),
			cfg:  newTestConfig(),
			want: wantDeployObjectsWithoutPorts(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertToWorkloads(tt.env, tt.cfg)
			if tt.wantErrMessage != "" {
				if err == nil {
					t.Fatalf("期望返回错误 %q, 实际返回 nil", tt.wantErrMessage)
				}
				if err.Error() != tt.wantErrMessage {
					t.Fatalf("错误信息期望 %q, 实际 %q", tt.wantErrMessage, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("不期望返回错误, 实际返回: %v", err)
			}
			assertDeployObjectsEqual(t, got, tt.want)
		})
	}
}

func TestConvertToWorkloads_EnvTypePassthrough(t *testing.T) {
	tests := []struct {
		name        string
		envType     domain.EnvironmentType
		wantEnvType domain.EnvironmentType
		wantLabel   string
	}{
		{
			name:        "test env passes through",
			envType:     domain.EnvironmentTypeTest,
			wantEnvType: domain.EnvironmentTypeTest,
			wantLabel:   "tstscope.dev",
		},
		{
			name:        "dev env passes through",
			envType:     domain.EnvironmentTypeDev,
			wantEnvType: domain.EnvironmentTypeDev,
			wantLabel:   "tstscope.dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnvWithType(t, tt.envType, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{{
					Name:     "api",
					App:      "apiapp",
					Image:    "apiapp:v1",
					Ports:    []domain.ArtifactPortSpec{{Name: "http", Port: 9090}},
					Replicas: 1,
					HTTP: &domain.ArtifactHTTPSpec{
						Hostnames: []string{"api.example.com"},
						Matches: []domain.HTTPRouteRule{{
							Backend: "http",
							Path: domain.HTTPPathRule{
								Type:  domain.HTTPPathRuleTypePathPrefix,
								Value: "/",
							},
						}},
					},
				}},
			})

			got, err := ConvertToWorkloads(env, newTestConfig())
			if err != nil {
				t.Fatalf("ConvertToWorkloads() unexpected error: %v", err)
			}
			if len(got.HTTPRoutes) != 1 {
				t.Fatalf("HTTPRoutes count = %d, want 1", len(got.HTTPRoutes))
			}
			if got.HTTPRoutes[0].EnvType != tt.wantEnvType {
				t.Fatalf("EnvType = %v, want %v", got.HTTPRoutes[0].EnvType, tt.wantEnvType)
			}
		})
	}
}

func TestConvertToWorkloads_StatefulArtifacts(t *testing.T) {
	tests := []struct {
		name string
		env  *domain.Environment
		want *DeployObjects
	}{
		{
			name: "stateful artifact creates stateful workload and instance routes",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{{
					Name:         "game-gateway",
					App:          "game",
					Image:        "game:v1",
					Replicas:     3,
					WorkloadKind: domain.WorkloadKindStateful,
					Ports:        []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
					HTTP: &domain.ArtifactHTTPSpec{
						Hostnames: []string{"gateway.example.com"},
						Matches: []domain.HTTPRouteRule{{
							Backend: "http",
							Path:    domain.HTTPPathRule{Type: domain.HTTPPathRuleTypePathPrefix, Value: "/"},
						}},
					},
				}},
			}),
			want: wantDeployObjectsWithStatefulArtifact(
				domain.EnvironmentTypeProd,
				3,
				[]string{"gateway.example.com"},
				[]*HTTPRoutePathMatch{{Type: HTTPPathMatchTypePathPrefix, Value: "/", BackendName: "http", BackendPort: 8080}},
			),
		},
		{
			name: "stateful artifact expands all hostnames per instance",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{{
					Name:         "game-gateway",
					App:          "game",
					Image:        "game:v1",
					Replicas:     2,
					WorkloadKind: domain.WorkloadKindStateful,
					Ports:        []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
					HTTP: &domain.ArtifactHTTPSpec{
						Hostnames: []string{"gateway.example.com", "gateway.internal.example.com"},
						Matches: []domain.HTTPRouteRule{{
							Backend: "http",
							Path:    domain.HTTPPathRule{Type: domain.HTTPPathRuleTypePathPrefix, Value: "/"},
						}},
					},
				}},
			}),
			want: wantDeployObjectsWithStatefulArtifact(
				domain.EnvironmentTypeProd,
				2,
				[]string{"gateway.example.com", "gateway.internal.example.com"},
				[]*HTTPRoutePathMatch{{Type: HTTPPathMatchTypePathPrefix, Value: "/", BackendName: "http", BackendPort: 8080}},
			),
		},
		{
			name: "stateful artifact without http only creates stateful workload",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{{
					Name:         "queue",
					App:          "worker",
					Image:        "worker:v1",
					Replicas:     2,
					WorkloadKind: domain.WorkloadKindStateful,
					Ports:        []domain.ArtifactPortSpec{{Name: "grpc", Port: 50051}},
				}},
			}),
			want: &DeployObjects{
				StatefulWorkloads: []*StatefulWorkload{{
					ServiceName:     "queue",
					EnvironmentName: "tstscope.dev",
					App:             "worker",
					Image:           "worker:v1",
					Replicas:        2,
					Ports: []*DeploymentPort{{
						Name: "grpc",
						Port: 50051,
					}},
				}},
			},
		},
		{
			name: "stateful artifact scales down instance routes with replicas",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{{
					Name:         "game-gateway",
					App:          "game",
					Image:        "game:v1",
					Replicas:     1,
					WorkloadKind: domain.WorkloadKindStateful,
					Ports:        []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
					HTTP: &domain.ArtifactHTTPSpec{
						Hostnames: []string{"gateway.example.com"},
						Matches: []domain.HTTPRouteRule{{
							Backend: "http",
							Path:    domain.HTTPPathRule{Type: domain.HTTPPathRuleTypePathPrefix, Value: "/"},
						}},
					},
				}},
			}),
			want: wantDeployObjectsWithStatefulArtifact(
				domain.EnvironmentTypeProd,
				1,
				[]string{"gateway.example.com"},
				[]*HTTPRoutePathMatch{{Type: HTTPPathMatchTypePathPrefix, Value: "/", BackendName: "http", BackendPort: 8080}},
			),
		},
		{
			name: "mixed stateless and stateful artifacts use separate paths",
			env: newTestEnv(t, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{
					{
						Name:     "api",
						App:      "platform",
						Image:    "platform:v1",
						Replicas: 2,
						Ports:    []domain.ArtifactPortSpec{{Name: "http", Port: 9090}},
						HTTP: &domain.ArtifactHTTPSpec{
							Hostnames: []string{"api.example.com"},
							Matches: []domain.HTTPRouteRule{{
								Backend: "http",
								Path: domain.HTTPPathRule{
									Type:  domain.HTTPPathRuleTypePathPrefix,
									Value: "/",
								},
							}},
						},
					},
					{
						Name:         "game-gateway",
						App:          "game",
						Image:        "game:v1",
						Replicas:     2,
						WorkloadKind: domain.WorkloadKindStateful,
						Ports:        []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
						HTTP: &domain.ArtifactHTTPSpec{
							Hostnames: []string{"gateway.example.com"},
							Matches: []domain.HTTPRouteRule{{
								Backend: "http",
								Path:    domain.HTTPPathRule{Type: domain.HTTPPathRuleTypePathPrefix, Value: "/"},
							}},
						},
					},
				},
			}),
			want: &DeployObjects{
				Deployments: []*DeploymentWorkload{{
					ServiceName:     "api",
					EnvironmentName: "tstscope.dev",
					App:             "platform",
					Image:           "platform:v1",
					Replicas:        2,
					Ports: []*DeploymentPort{{
						Name: "http",
						Port: 9090,
					}},
				}},
				HTTPRoutes: []*HTTPRouteWorkload{{
					ServiceName:      "api",
					EnvironmentName:  "tstscope.dev",
					App:              "platform",
					EnvType:          domain.EnvironmentTypeProd,
					Hostnames:        []string{"api.example.com"},
					BackendService:   newObjectName(WorkloadKindService, "platform", "api"),
					GatewayName:      "test-gateway",
					GatewayNamespace: "test-gw-ns",
					Matches: []*HTTPRoutePathMatch{{
						Type:        HTTPPathMatchTypePathPrefix,
						Value:       "/",
						BackendName: "http",
						BackendPort: 9090,
					}},
				}},
				StatefulWorkloads: []*StatefulWorkload{{
					ServiceName:     "game-gateway",
					EnvironmentName: "tstscope.dev",
					App:             "game",
					Image:           "game:v1",
					Replicas:        2,
					Ports: []*DeploymentPort{{
						Name: "http",
						Port: 8080,
					}},
					Hostnames: []string{"gateway.example.com"},
				}},
				InstanceRoutes: buildWantStatefulInstanceRoutes(domain.EnvironmentTypeProd, 2, []string{"gateway.example.com"}, []*HTTPRoutePathMatch{{Type: HTTPPathMatchTypePathPrefix, Value: "/", BackendName: "http", BackendPort: 8080}}),
			},
		},
		{
			name: "non-prod stateful artifact keeps env type on instance routes",
			env: newTestEnvWithType(t, domain.EnvironmentTypeTest, &domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{{
					Name:         "game-gateway",
					App:          "game",
					Image:        "game:v1",
					Replicas:     2,
					WorkloadKind: domain.WorkloadKindStateful,
					Ports:        []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
					HTTP: &domain.ArtifactHTTPSpec{
						Hostnames: []string{"gateway.example.com"},
						Matches: []domain.HTTPRouteRule{{
							Backend: "http",
							Path:    domain.HTTPPathRule{Type: domain.HTTPPathRuleTypePathPrefix, Value: "/"},
						}},
					},
				}},
			}),
			want: wantDeployObjectsWithStatefulArtifact(
				domain.EnvironmentTypeTest,
				2,
				[]string{"gateway.example.com"},
				[]*HTTPRoutePathMatch{{Type: HTTPPathMatchTypePathPrefix, Value: "/", BackendName: "http", BackendPort: 8080}},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			cfg := newTestConfig()

			// when
			got, err := ConvertToWorkloads(tt.env, cfg)

			// then
			if err != nil {
				t.Fatalf("ConvertToWorkloads() unexpected error: %v", err)
			}
			assertDeployObjectsEqual(t, got, tt.want)
		})
	}
}

func TestConvertToWorkloads_StatefulArtifactUsesMatchedBackendPort(t *testing.T) {
	// given
	env := newTestEnv(t, &domain.DesiredState{
		Artifacts: []*domain.ArtifactSpec{{
			Name:         "game-gateway",
			App:          "game",
			Image:        "game:v1",
			Replicas:     2,
			WorkloadKind: domain.WorkloadKindStateful,
			Ports:        []domain.ArtifactPortSpec{{Name: "metrics", Port: 9090}, {Name: "grpc", Port: 50051}},
			HTTP: &domain.ArtifactHTTPSpec{
				Hostnames: []string{"gateway.example.com"},
				Matches: []domain.HTTPRouteRule{{
					Backend: "grpc",
					Path:    domain.HTTPPathRule{Type: domain.HTTPPathRuleTypePathPrefix, Value: "/rpc"},
				}},
			},
		}},
	})

	// when
	got, err := ConvertToWorkloads(env, newTestConfig())

	// then
	if err != nil {
		t.Fatalf("ConvertToWorkloads() unexpected error: %v", err)
	}
	if len(got.InstanceRoutes) != 2 {
		t.Fatalf("InstanceRoutes count = %d, want 2", len(got.InstanceRoutes))
	}
	for i, route := range got.InstanceRoutes {
		if len(route.Matches) != 1 {
			t.Fatalf("InstanceRoutes[%d].Matches count = %d, want 1", i, len(route.Matches))
		}
		if route.Matches[0].BackendName != "grpc" {
			t.Fatalf("InstanceRoutes[%d].Matches[0].BackendName = %s, want grpc", i, route.Matches[0].BackendName)
		}
		if route.Matches[0].BackendPort != 50051 {
			t.Fatalf("InstanceRoutes[%d].Matches[0].BackendPort = %d, want 50051", i, route.Matches[0].BackendPort)
		}
		if route.Matches[0].Type != HTTPPathMatchTypePathPrefix {
			t.Fatalf("InstanceRoutes[%d].Matches[0].Type = %v, want %v", i, route.Matches[0].Type, HTTPPathMatchTypePathPrefix)
		}
		if route.Matches[0].Value != "/rpc" {
			t.Fatalf("InstanceRoutes[%d].Matches[0].Value = %s, want /rpc", i, route.Matches[0].Value)
		}
	}
}

func assertDeployObjectsEqual(t *testing.T, got *DeployObjects, want *DeployObjects) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ConvertToWorkloads() = %#v, want %#v", got, want)
	}
}

func wantDeployObjectsWithServicesOnly() *DeployObjects {
	return &DeployObjects{
		Deployments: []*DeploymentWorkload{{
			ServiceName:     "web",
			EnvironmentName: "tstscope.dev",
			App:             "webapp",
			Image:           "webapp:v1",
			Replicas:        3,
			Ports: []*DeploymentPort{{
				Name: "http",
				Port: 8080,
			}},
		}},
	}
}

func wantDeployObjectsWithTLSEnabled() *DeployObjects {
	return &DeployObjects{
		Deployments: []*DeploymentWorkload{{
			TLSEnabled:      true,
			ServiceName:     "secure-svc",
			EnvironmentName: "tstscope.dev",
			App:             "secureapp",
			Image:           "secureapp:v1",
			Replicas:        1,
			Ports: []*DeploymentPort{{
				Name: "https",
				Port: 8443,
			}},
		}},
	}
}

func wantDeployObjectsWithMongoDBInfra() *DeployObjects {
	return &DeployObjects{
		Deployments: []*DeploymentWorkload{{
			ServiceName:     "svc1",
			EnvironmentName: "tstscope.dev",
			App:             "app1",
			Image:           "app1:v1",
			Replicas:        1,
			Ports: []*DeploymentPort{{
				Name: "http",
				Port: 8080,
			}},
		}},
		MongoDBWorkloads: []*MongoDBWorkload{{
			ServiceName:     "mongo1",
			EnvironmentName: "tstscope.dev",
			App:             "myapp",
			ProfileName:     "dev-single",
			Persistence:     PersistenceConfig{Enabled: true},
		}},
	}
}

func wantDeployObjectsWithMongoDBWithoutPersistence() *DeployObjects {
	return &DeployObjects{
		Deployments: []*DeploymentWorkload{{
			ServiceName:     "svc1",
			EnvironmentName: "tstscope.dev",
			App:             "app1",
			Image:           "app1:v1",
			Replicas:        1,
			Ports: []*DeploymentPort{{
				Name: "http",
				Port: 8080,
			}},
		}},
		MongoDBWorkloads: []*MongoDBWorkload{{
			ServiceName:     "cache",
			EnvironmentName: "tstscope.dev",
			App:             "myapp",
			ProfileName:     "dev-single",
			Persistence:     PersistenceConfig{Enabled: false},
		}},
	}
}

func wantDeployObjectsWithSingleRuleHTTPRoute() *DeployObjects {
	deployment := &DeploymentWorkload{
		ServiceName:     "api",
		EnvironmentName: "tstscope.dev",
		App:             "apiapp",
		Image:           "apiapp:v1",
		Replicas:        2,
		Ports: []*DeploymentPort{{
			Name: "http",
			Port: 9090,
		}},
	}

	return &DeployObjects{
		Deployments: []*DeploymentWorkload{deployment},
		HTTPRoutes: []*HTTPRouteWorkload{{
			ServiceName:      "api",
			EnvironmentName:  "tstscope.dev",
			App:              "apiapp",
			EnvType:          domain.EnvironmentTypeProd,
			Hostnames:        []string{"api.example.com"},
			BackendService:   deployment.ServiceResourceName(),
			GatewayName:      "test-gateway",
			GatewayNamespace: "test-gw-ns",
			Matches: []*HTTPRoutePathMatch{{
				Type:        HTTPPathMatchTypePathPrefix,
				Value:       "/v1",
				BackendName: "http",
				BackendPort: 9090,
			}},
		}},
	}
}

func wantDeployObjectsWithMultipleRuleHTTPRoute() *DeployObjects {
	firstDeployment := &DeploymentWorkload{
		ServiceName:     "svc1",
		EnvironmentName: "tstscope.dev",
		App:             "app1",
		Image:           "app1:v1",
		Replicas:        1,
		Ports: []*DeploymentPort{{
			Name: "http",
			Port: 8080,
		}, {
			Name: "grpc",
			Port: 50051,
		}},
	}
	return &DeployObjects{
		Deployments: []*DeploymentWorkload{firstDeployment},
		HTTPRoutes: []*HTTPRouteWorkload{{
			ServiceName:      "svc1",
			EnvironmentName:  "tstscope.dev",
			App:              "app1",
			EnvType:          domain.EnvironmentTypeProd,
			Hostnames:        []string{"multi.example.com"},
			BackendService:   firstDeployment.ServiceResourceName(),
			GatewayName:      "test-gateway",
			GatewayNamespace: "test-gw-ns",
			Matches: []*HTTPRoutePathMatch{
				{
					Type:        HTTPPathMatchTypePathPrefix,
					Value:       "/api",
					BackendName: "http",
					BackendPort: 8080,
				},
				{
					Type:        HTTPPathMatchTypePathPrefix,
					Value:       "/grpc",
					BackendName: "grpc",
					BackendPort: 50051,
				},
			},
		}},
	}
}

func wantDeployObjectsWithAllWorkloadTypes() *DeployObjects {
	webDeployment := &DeploymentWorkload{
		ServiceName:     "web",
		EnvironmentName: "tstscope.dev",
		App:             "webapp",
		Image:           "webapp:v1",
		Replicas:        2,
		Ports: []*DeploymentPort{{
			Name: "http",
			Port: 8080,
		}},
	}
	apiDeployment := &DeploymentWorkload{
		ServiceName:     "api",
		EnvironmentName: "tstscope.dev",
		App:             "apiapp",
		Image:           "apiapp:v2",
		Replicas:        1,
		Ports: []*DeploymentPort{{
			Name: "grpc",
			Port: 50051,
		}},
	}

	return &DeployObjects{
		Deployments: []*DeploymentWorkload{webDeployment, apiDeployment},
		MongoDBWorkloads: []*MongoDBWorkload{{
			ServiceName:     "mongo",
			EnvironmentName: "tstscope.dev",
			App:             "webapp",
			ProfileName:     "dev-single",
			Persistence:     PersistenceConfig{Enabled: true},
		}},
		HTTPRoutes: []*HTTPRouteWorkload{{
			ServiceName:      "web",
			EnvironmentName:  "tstscope.dev",
			App:              "webapp",
			EnvType:          domain.EnvironmentTypeProd,
			Hostnames:        []string{"web.example.com"},
			BackendService:   webDeployment.ServiceResourceName(),
			GatewayName:      "test-gateway",
			GatewayNamespace: "test-gw-ns",
			Matches: []*HTTPRoutePathMatch{{
				Type:        HTTPPathMatchTypePathPrefix,
				Value:       "/",
				BackendName: "http",
				BackendPort: 8080,
			}},
		}},
	}
}

func wantDeployObjectsWithoutPorts() *DeployObjects {
	return &DeployObjects{
		Deployments: []*DeploymentWorkload{{
			ServiceName:     "worker",
			EnvironmentName: "tstscope.dev",
			App:             "workerapp",
			Image:           "workerapp:v1",
			Replicas:        1,
			Ports:           nil,
		}},
	}
}

func wantDeployObjectsWithStatefulArtifact(envType domain.EnvironmentType, replicas int32, hostnames []string, matches []*HTTPRoutePathMatch) *DeployObjects {
	return &DeployObjects{
		StatefulWorkloads: []*StatefulWorkload{{
			ServiceName:     "game-gateway",
			EnvironmentName: "tstscope.dev",
			App:             "game",
			Image:           "game:v1",
			Replicas:        replicas,
			Ports: []*DeploymentPort{{
				Name: "http",
				Port: 8080,
			}},
			Hostnames: hostnames,
		}},
		InstanceRoutes: buildWantStatefulInstanceRoutes(envType, replicas, hostnames, matches),
	}
}

func buildWantStatefulInstanceRoutes(envType domain.EnvironmentType, replicas int32, hostnames []string, matches []*HTTPRoutePathMatch) []*HTTPRouteWorkload {
	if replicas == 0 {
		return nil
	}

	routes := make([]*HTTPRouteWorkload, 0, replicas)
	for i := 0; i < int(replicas); i++ {
		routes = append(routes, &HTTPRouteWorkload{
			ServiceName:      "game-gateway",
			EnvironmentName:  "tstscope.dev",
			App:              "game",
			Hostnames:        buildExpandedHostnames("game-gateway", i, hostnames),
			BackendService:   newInstanceObjectName(WorkloadKindInstanceService, "tstscope.dev", "game-gateway", i),
			GatewayName:      "test-gateway",
			GatewayNamespace: "test-gw-ns",
			EnvType:          envType,
			Matches:          matches,
		})
	}

	return routes
}

func buildExpandedHostnames(serviceName string, instanceIndex int, hostnames []string) []string {
	if len(hostnames) == 0 {
		return nil
	}

	expandedHostnames := make([]string, 0, len(hostnames))
	for _, hostname := range hostnames {
		expandedHostnames = append(expandedHostnames, fmt.Sprintf("%s-%d.%s", serviceName, instanceIndex, hostname))
	}

	return expandedHostnames
}

func Test_convertPorts(t *testing.T) {
	tests := []struct {
		name  string
		ports []domain.ArtifactPortSpec
		want  []*DeploymentPort
	}{
		{
			name:  "nil ports returns nil",
			ports: nil,
			want:  nil,
		},
		{
			name:  "empty ports returns nil",
			ports: []domain.ArtifactPortSpec{},
			want:  nil,
		},
		{
			name: "single port",
			ports: []domain.ArtifactPortSpec{
				{Name: "http", Port: 8080},
			},
			want: []*DeploymentPort{
				{Name: "http", Port: 8080},
			},
		},
		{
			name: "multiple ports",
			ports: []domain.ArtifactPortSpec{
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
