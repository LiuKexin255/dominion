package k8s

import (
	"errors"
	"reflect"
	"testing"

	"dominion/tools/deploy/pkg/config"
)

func TestDeploymentWorkloadValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   *DeploymentWorkload
		wantErr bool
	}{
		{
			name: "valid minimal workload",
			input: &DeploymentWorkload{
				ServiceName:     "hello",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				Desc:            "hello service",
				Image:           "registry.local/hello:latest",
			},
		},
		{name: "nil workload", wantErr: true},
		{
			name: "missing service name",
			input: &DeploymentWorkload{
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				Desc:            "hello service",
				Image:           "registry.local/hello:latest",
			},
			wantErr: true,
		},
		{
			name: "missing environment name",
			input: &DeploymentWorkload{
				ServiceName: "hello",
				App:         "grpc-hello-world",
				Desc:        "hello service",
				Image:       "registry.local/hello:latest",
			},
			wantErr: true,
		},
		{
			name: "missing app",
			input: &DeploymentWorkload{
				ServiceName:     "hello",
				EnvironmentName: "dev",
				Desc:            "hello service",
				Image:           "registry.local/hello:latest",
			},
			wantErr: true,
		},
		{
			name: "missing desc",
			input: &DeploymentWorkload{
				ServiceName:     "hello",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				Image:           "registry.local/hello:latest",
			},
			wantErr: true,
		},
		{
			name: "missing image",
			input: &DeploymentWorkload{
				ServiceName:     "hello",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				Desc:            "hello service",
			},
			wantErr: true,
		},
		{
			name: "invalid replicas",
			input: &DeploymentWorkload{
				ServiceName:     "hello",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				Desc:            "hello service",
				Image:           "registry.local/hello:latest",
				Replicas:        -1,
			},
			wantErr: true,
		},
		{
			name: "empty port name",
			input: &DeploymentWorkload{
				ServiceName:     "hello",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				Desc:            "hello service",
				Image:           "registry.local/hello:latest",
				Ports:           []*DeploymentPort{{Name: "", Port: 8080}},
			},
			wantErr: true,
		},
		{
			name: "invalid port range",
			input: &DeploymentWorkload{
				ServiceName:     "hello",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				Desc:            "hello service",
				Image:           "registry.local/hello:latest",
				Ports:           []*DeploymentPort{{Name: "http", Port: 70000}},
			},
			wantErr: true,
		},
		{
			name: "name too long",
			input: &DeploymentWorkload{
				ServiceName:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				EnvironmentName: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				App:             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Desc:            "desc",
				Image:           "repo:tag",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() failed: %v", err)
			}
		})
	}
}

func TestHTTPRouteWorkloadValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   *HTTPRouteWorkload
		wantErr bool
	}{
		{
			name: "valid",
			input: &HTTPRouteWorkload{
				ServiceName:      "gateway",
				EnvironmentName:  "dev",
				App:              "grpc-hello-world",
				BackendService:   "svc",
				GatewayName:      "gw",
				GatewayNamespace: "infra",
				Matches: []*HTTPRoutePathMatch{{
					Type:        config.HTTPPathMatchTypePrefix,
					Value:       "/v1",
					BackendPort: 80,
				}},
			},
		},
		{name: "nil workload", wantErr: true},
		{
			name: "name too long",
			input: &HTTPRouteWorkload{
				ServiceName:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				EnvironmentName:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				App:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				BackendService:   "svc",
				GatewayName:      "gw",
				GatewayNamespace: "infra",
				Matches: []*HTTPRoutePathMatch{{
					Type:        config.HTTPPathMatchTypePrefix,
					Value:       "/v1",
					BackendPort: 80,
				}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() failed: %v", err)
			}
		})
	}
}

func TestWorkloadKindMongoDBAndStorageKinds(t *testing.T) {
	tests := []struct {
		name string
		got  WorkloadKind
		want WorkloadKind
	}{
		{name: "mongo", got: WorkloadKindMongoDB, want: "mongo"},
		{name: "pvc", got: WorkloadKindPVC, want: "pvc"},
		{name: "secret", got: WorkloadKindSecret, want: "secret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}
func Test_newObjectName(t *testing.T) {
	tests := []struct {
		name        string
		kind        WorkloadKind
		fullEnvName string
		serviceName string
		want        string
	}{
		{
			name:        "normal",
			kind:        WorkloadKindDeployment,
			fullEnvName: "grpc-hello-world.dev",
			serviceName: "gateway",
			want:        "dp-grpc-hello-world-dev-gateway-" + shortNameHash("grpc-hello-world.dev"),
		},
		{
			name:        "normalize and sanitize",
			kind:        WorkloadKindService,
			fullEnvName: "GRPC_HELLO_WORLD.dev",
			serviceName: "gateway@v1",
			want:        "svc-grpc-hello-world-dev-gateway-v1-" + shortNameHash("GRPC_HELLO_WORLD.dev"),
		},
		{
			name:        "only kind when all parts empty",
			kind:        WorkloadKindHTTPRoute,
			fullEnvName: "",
			serviceName: "",
			want:        "route-" + shortNameHash(""),
		},
		{
			name:        "fallback to unknown kind",
			kind:        "",
			fullEnvName: "app.dev",
			serviceName: "svc",
			want:        "unknown-app-dev-svc-" + shortNameHash("app.dev"),
		},
		{
			name:        "skip empty normalized part",
			kind:        WorkloadKindDeployment,
			fullEnvName: "---.dev",
			serviceName: "svc",
			want:        "dp-dev-svc-" + shortNameHash("---.dev"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newObjectName(tt.kind, tt.fullEnvName, tt.serviceName)
			if got != tt.want {
				t.Errorf("newObjectName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_toDeploymentPorts(t *testing.T) {
	tests := []struct {
		name  string
		ports []*config.ServiceArtifactPort
		want  []*DeploymentPort
	}{
		{
			name:  "nil input",
			ports: nil,
			want:  nil,
		},
		{
			name: "map and skip nil",
			ports: []*config.ServiceArtifactPort{
				{Name: "http", Port: 80},
				nil,
				{Name: "grpc", Port: 50051},
			},
			want: []*DeploymentPort{
				{Name: "http", Port: 80},
				{Name: "grpc", Port: 50051},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toDeploymentPorts(tt.ports)
			if tt.want == nil {
				if len(got) != 0 {
					t.Errorf("toDeploymentPorts() len = %d, want 0", len(got))
				}
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("toDeploymentPorts() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_resolveArtifactByName(t *testing.T) {
	tests := []struct {
		name         string
		serviceCfg   *config.ServiceConfig
		artifactName string
		want         *config.ServiceArtifact
		wantErr      bool
		wantNotFound bool
	}{
		{
			name: "found",
			serviceCfg: &config.ServiceConfig{
				Artifacts: []*config.ServiceArtifact{{Name: "gateway", Type: config.ServiceArtifactTypeDeployment}},
			},
			artifactName: "gateway",
			want:         &config.ServiceArtifact{Name: "gateway", Type: config.ServiceArtifactTypeDeployment},
		},
		{
			name:         "nil config",
			artifactName: "gateway",
			wantErr:      true,
		},
		{
			name: "empty artifact name",
			serviceCfg: &config.ServiceConfig{
				Artifacts: []*config.ServiceArtifact{{Name: "gateway"}},
			},
			artifactName: " ",
			wantErr:      true,
		},
		{
			name: "artifact not found",
			serviceCfg: &config.ServiceConfig{
				Artifacts: []*config.ServiceArtifact{{Name: "gateway"}},
			},
			artifactName: "service",
			wantErr:      true,
			wantNotFound: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := resolveArtifactByName(tt.serviceCfg, tt.artifactName)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("resolveArtifactByName() failed: %v", gotErr)
				}
				if tt.wantNotFound && !errors.Is(gotErr, config.ErrNotFound) {
					t.Fatalf("resolveArtifactByName() expected ErrNotFound, got %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("resolveArtifactByName() succeeded unexpectedly")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("resolveArtifactByName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_newDeploymentWorkload_TLSEnabled(t *testing.T) {
	tests := []struct {
		name        string
		artifactTLS bool
		wantEnabled bool
	}{
		{
			name:        "carries tls enabled flag",
			artifactTLS: true,
			wantEnabled: true,
		},
		{
			name:        "keeps tls disabled by default",
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serviceCfg := &config.ServiceConfig{
				Name: "gateway",
				App:  "grpc-hello-world",
				Desc: "gateway service",
			}
			artifact := &config.ServiceArtifact{
				Name:   "gateway",
				Type:   config.ServiceArtifactTypeDeployment,
				Target: "//some/path:gateway_image",
				TLS:    tt.artifactTLS,
				Ports: []*config.ServiceArtifactPort{{
					Name: "grpc",
					Port: 50051,
				}},
			}

			got, err := newDeploymentWorkload(serviceCfg, artifact, "dev", "registry.example.com/team/gateway:latest")
			if err != nil {
				t.Fatalf("newDeploymentWorkload() failed: %v", err)
			}

			if got.TLSEnabled != tt.wantEnabled {
				t.Fatalf("deployment tls enabled = %t, want %t", got.TLSEnabled, tt.wantEnabled)
			}
		})
	}
}

func Test_buildHTTPRoutePathMatches(t *testing.T) {
	tests := []struct {
		name              string
		ports             []*DeploymentPort
		deployHTTPMatches []*config.DeployHTTPMatch
		want              []*HTTPRoutePathMatch
		wantErr           bool
	}{
		{
			name: "map matches and skip nil",
			ports: []*DeploymentPort{
				{Name: "http", Port: 80},
				nil,
				{Name: "grpc", Port: 50051},
			},
			deployHTTPMatches: []*config.DeployHTTPMatch{
				{Backend: " HTTP ", Path: config.DeployHTTPPathMatch{Type: config.HTTPPathMatchTypePrefix, Value: " /v1 "}},
				nil,
				{Backend: "grpc", Path: config.DeployHTTPPathMatch{Type: config.HTTPPathMatchTypePrefix, Value: "/grpc"}},
			},
			want: []*HTTPRoutePathMatch{
				{Type: config.HTTPPathMatchTypePrefix, Value: "/v1", BackendName: "HTTP", BackendPort: 80},
				{Type: config.HTTPPathMatchTypePrefix, Value: "/grpc", BackendName: "grpc", BackendPort: 50051},
			},
		},
		{
			name:              "missing backend",
			ports:             []*DeploymentPort{{Name: "http", Port: 80}},
			deployHTTPMatches: []*config.DeployHTTPMatch{{Backend: "   ", Path: config.DeployHTTPPathMatch{Type: config.HTTPPathMatchTypePrefix, Value: "/v1"}}},
			wantErr:           true,
		},
		{
			name:              "backend not found",
			ports:             []*DeploymentPort{{Name: "http", Port: 80}},
			deployHTTPMatches: []*config.DeployHTTPMatch{{Backend: "grpc", Path: config.DeployHTTPPathMatch{Type: config.HTTPPathMatchTypePrefix, Value: "/v1"}}},
			wantErr:           true,
		},
		{
			name:              "empty matches",
			ports:             []*DeploymentPort{{Name: "http", Port: 80}},
			deployHTTPMatches: nil,
			want:              nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := buildHTTPRoutePathMatches(tt.ports, tt.deployHTTPMatches)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("buildHTTPRoutePathMatches() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("buildHTTPRoutePathMatches() succeeded unexpectedly")
			}
			if tt.want == nil {
				if len(got) != 0 {
					t.Errorf("buildHTTPRoutePathMatches() len = %d, want 0", len(got))
				}
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildHTTPRoutePathMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeploymentWorkloadNewHTTPRouteWorkload(t *testing.T) {
	tests := []struct {
		name          string
		workload      *DeploymentWorkload
		deployService *config.DeployService
		k8sConfig     *K8sConfig
		want          *HTTPRouteWorkload
		wantErr       bool
	}{
		{
			name: "valid",
			workload: &DeploymentWorkload{
				ServiceName:     "gateway",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				Desc:            "gateway service",
				Image:           "registry.example.com/team/gateway:latest",
				Ports:           []*DeploymentPort{{Name: "http", Port: 80}},
			},
			deployService: &config.DeployService{
				HTTP: config.DeployHTTP{
					Hostnames: []string{"hello.example.com"},
					Matches: []*config.DeployHTTPMatch{{
						Backend: "http",
						Path: config.DeployHTTPPathMatch{
							Type:  config.HTTPPathMatchTypePrefix,
							Value: "/v1",
						},
					}},
				},
			},
			k8sConfig: &K8sConfig{Gateway: GatewayConfig{Name: "gw", Namespace: "infra"}},
			want: &HTTPRouteWorkload{
				ServiceName:      "gateway",
				EnvironmentName:  "dev",
				App:              "grpc-hello-world",
				Hostnames:        []string{"hello.example.com"},
				Matches:          []*HTTPRoutePathMatch{{Type: config.HTTPPathMatchTypePrefix, Value: "/v1", BackendName: "http", BackendPort: 80}},
				BackendService:   (&DeploymentWorkload{ServiceName: "gateway", EnvironmentName: "dev"}).ServiceResourceName(),
				GatewayName:      "gw",
				GatewayNamespace: "infra",
			},
		},
		{
			name: "backend not found",
			workload: &DeploymentWorkload{
				ServiceName:     "gateway",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				Desc:            "gateway service",
				Image:           "registry.example.com/team/gateway:latest",
				Ports:           []*DeploymentPort{{Name: "http", Port: 80}},
			},
			deployService: &config.DeployService{
				HTTP: config.DeployHTTP{Matches: []*config.DeployHTTPMatch{{
					Backend: "grpc",
					Path:    config.DeployHTTPPathMatch{Type: config.HTTPPathMatchTypePrefix, Value: "/v1"},
				}}},
			},
			k8sConfig: &K8sConfig{Gateway: GatewayConfig{Name: "gw", Namespace: "infra"}},
			wantErr:   true,
		},
		{
			name: "missing gateway name",
			workload: &DeploymentWorkload{
				ServiceName:     "gateway",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				Desc:            "gateway service",
				Image:           "registry.example.com/team/gateway:latest",
				Ports:           []*DeploymentPort{{Name: "http", Port: 80}},
			},
			deployService: &config.DeployService{
				HTTP: config.DeployHTTP{Matches: []*config.DeployHTTPMatch{{
					Backend: "http",
					Path:    config.DeployHTTPPathMatch{Type: config.HTTPPathMatchTypePrefix, Value: "/v1"},
				}}},
			},
			k8sConfig: &K8sConfig{Gateway: GatewayConfig{Name: "", Namespace: "infra"}},
			wantErr:   true,
		},
		{
			name: "empty matches",
			workload: &DeploymentWorkload{
				ServiceName:     "gateway",
				EnvironmentName: "dev",
				App:             "grpc-hello-world",
				Desc:            "gateway service",
				Image:           "registry.example.com/team/gateway:latest",
				Ports:           []*DeploymentPort{{Name: "http", Port: 80}},
			},
			deployService: &config.DeployService{HTTP: config.DeployHTTP{Matches: nil}},
			k8sConfig:     &K8sConfig{Gateway: GatewayConfig{Name: "gw", Namespace: "infra"}},
			wantErr:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubLoadK8sConfig(t, tt.k8sConfig)

			got, gotErr := tt.workload.NewHTTPRouteWorkload(tt.deployService)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("NewHTTPRouteWorkload() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("NewHTTPRouteWorkload() succeeded unexpectedly")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewHTTPRouteWorkload() = %v, want %v", got, tt.want)
			}
		})
	}
}
