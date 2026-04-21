package k8s

import (
	"strings"
	"testing"
)

func TestStatefulWorkload_Validate(t *testing.T) {
	tests := []struct {
		name     string
		workload StatefulWorkload
		wantErr  bool
	}{
		{name: "valid", workload: StatefulWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", Image: "repo/app:v1", Replicas: 3, Ports: []*DeploymentPort{{Name: "http", Port: 8080}}}},
		{name: "valid with env", workload: StatefulWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", Image: "repo/app:v1", Replicas: 3, Env: map[string]string{"FOO": "bar", "BAZ": "qux"}}},
		{name: "zero replicas", workload: StatefulWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", Image: "repo/app:v1", Replicas: 0}},
		{name: "missing service", workload: StatefulWorkload{EnvironmentName: "dev", App: "app", Image: "repo/app:v1"}, wantErr: true},
		{name: "missing env", workload: StatefulWorkload{ServiceName: "svc", App: "app", Image: "repo/app:v1"}, wantErr: true},
		{name: "missing app", workload: StatefulWorkload{ServiceName: "svc", EnvironmentName: "dev", Image: "repo/app:v1"}, wantErr: true},
		{name: "missing image", workload: StatefulWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app"}, wantErr: true},
		{name: "negative replicas", workload: StatefulWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", Image: "repo/app:v1", Replicas: -1}, wantErr: true},
		{name: "invalid port", workload: StatefulWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", Image: "repo/app:v1", Ports: []*DeploymentPort{{Name: "http", Port: 0}}}, wantErr: true},
		{name: "nil port", workload: StatefulWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", Image: "repo/app:v1", Ports: []*DeploymentPort{nil}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.workload.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestStatefulWorkload_Validate_NameTooLong(t *testing.T) {
	workload := StatefulWorkload{
		ServiceName:     strings.Repeat("s", 60),
		EnvironmentName: "dev",
		App:             "app",
		Image:           "repo/app:v1",
	}
	err := workload.Validate()
	if err == nil {
		t.Fatalf("Validate() expected error for long name")
	}
}

func TestDeploymentWorkload_Validate(t *testing.T) {
	tests := []struct {
		name     string
		workload DeploymentWorkload
		wantErr  bool
	}{
		{name: "valid", workload: DeploymentWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", Image: "repo/app:v1", Replicas: 1, Ports: []*DeploymentPort{{Name: "http", Port: 8080}}}},
		{name: "valid with env", workload: DeploymentWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", Image: "repo/app:v1", Replicas: 1, Env: map[string]string{"FOO": "bar", "BAZ": "qux"}}},
		{name: "missing service", workload: DeploymentWorkload{EnvironmentName: "dev", App: "app", Image: "repo/app:v1"}, wantErr: true},
		{name: "missing env", workload: DeploymentWorkload{ServiceName: "svc", App: "app", Image: "repo/app:v1"}, wantErr: true},
		{name: "missing app", workload: DeploymentWorkload{ServiceName: "svc", EnvironmentName: "dev", Image: "repo/app:v1"}, wantErr: true},
		{name: "missing image", workload: DeploymentWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app"}, wantErr: true},
		{name: "negative replicas", workload: DeploymentWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", Image: "repo/app:v1", Replicas: -1}, wantErr: true},
		{name: "invalid port", workload: DeploymentWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", Image: "repo/app:v1", Ports: []*DeploymentPort{{Name: "http", Port: 0}}}, wantErr: true},
		{name: "nil port", workload: DeploymentWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", Image: "repo/app:v1", Ports: []*DeploymentPort{nil}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.workload.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestHTTPRouteWorkload_Validate(t *testing.T) {
	tests := []struct {
		name     string
		workload HTTPRouteWorkload
		wantErr  bool
	}{
		{name: "valid", workload: HTTPRouteWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", BackendService: "svc", GatewayName: "gw", GatewayNamespace: "default", Matches: []*HTTPRoutePathMatch{{Type: HTTPPathMatchTypePathPrefix, Value: "/", BackendPort: 8080}}}},
		{name: "missing service", workload: HTTPRouteWorkload{EnvironmentName: "dev", App: "app", BackendService: "svc", GatewayName: "gw", GatewayNamespace: "default", Matches: []*HTTPRoutePathMatch{{Type: HTTPPathMatchTypePathPrefix, Value: "/", BackendPort: 8080}}}, wantErr: true},
		{name: "missing backend service", workload: HTTPRouteWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", GatewayName: "gw", GatewayNamespace: "default", Matches: []*HTTPRoutePathMatch{{Type: HTTPPathMatchTypePathPrefix, Value: "/", BackendPort: 8080}}}, wantErr: true},
		{name: "missing matches", workload: HTTPRouteWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", BackendService: "svc", GatewayName: "gw", GatewayNamespace: "default"}, wantErr: true},
		{name: "nil match", workload: HTTPRouteWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", BackendService: "svc", GatewayName: "gw", GatewayNamespace: "default", Matches: []*HTTPRoutePathMatch{nil}}, wantErr: true},
		{name: "unspecified type", workload: HTTPRouteWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", BackendService: "svc", GatewayName: "gw", GatewayNamespace: "default", Matches: []*HTTPRoutePathMatch{{Value: "/", BackendPort: 8080}}}, wantErr: true},
		{name: "invalid backend port", workload: HTTPRouteWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", BackendService: "svc", GatewayName: "gw", GatewayNamespace: "default", Matches: []*HTTPRoutePathMatch{{Type: HTTPPathMatchTypePathPrefix, Value: "/", BackendPort: 70000}}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.workload.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestMongoDBWorkload_Validate(t *testing.T) {
	tests := []struct {
		name     string
		workload MongoDBWorkload
		wantErr  bool
	}{
		{name: "valid", workload: MongoDBWorkload{ServiceName: "mongo", EnvironmentName: "dev", App: "app", ProfileName: "dev-single", Persistence: PersistenceConfig{Enabled: true}}},
		{name: "missing service", workload: MongoDBWorkload{EnvironmentName: "dev", App: "app", ProfileName: "dev-single"}, wantErr: true},
		{name: "missing env", workload: MongoDBWorkload{ServiceName: "mongo", App: "app", ProfileName: "dev-single"}, wantErr: true},
		{name: "missing app", workload: MongoDBWorkload{ServiceName: "mongo", EnvironmentName: "dev", ProfileName: "dev-single"}, wantErr: true},
		{name: "missing profile", workload: MongoDBWorkload{ServiceName: "mongo", EnvironmentName: "dev", App: "app"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.workload.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestWorkloadNameMethods(t *testing.T) {
	if got := (&DeploymentWorkload{ServiceName: "svc", App: "app"}).WorkloadName(); got == "" {
		t.Fatalf("WorkloadName() = empty")
	}
	if got := (&DeploymentWorkload{ServiceName: "svc", App: "app"}).ServiceResourceName(); got == "" {
		t.Fatalf("ServiceResourceName() = empty")
	}
	if got := (&HTTPRouteWorkload{ServiceName: "svc", App: "app"}).ResourceName(); got == "" {
		t.Fatalf("ResourceName() = empty")
	}
	if got := (&MongoDBWorkload{ServiceName: "mongo", App: "app"}).ResourceName(); got == "" {
		t.Fatalf("ResourceName() = empty")
	}
	if got := (&StatefulWorkload{ServiceName: "svc", App: "app"}).WorkloadName(); got == "" {
		t.Fatalf("WorkloadName() = empty")
	}
	if got := (&StatefulWorkload{ServiceName: "svc", App: "app"}).ServiceResourceName(); got == "" {
		t.Fatalf("ServiceResourceName() = empty")
	}
}

func TestStatefulWorkload_NilReceiver(t *testing.T) {
	var w *StatefulWorkload
	if got := w.WorkloadName(); got != "" {
		t.Fatalf("nil WorkloadName() = %q, want empty", got)
	}
	if got := w.ServiceResourceName(); got != "" {
		t.Fatalf("nil ServiceResourceName() = %q, want empty", got)
	}
	if err := w.Validate(); err == nil {
		t.Fatalf("nil Validate() expected error")
	}
}
