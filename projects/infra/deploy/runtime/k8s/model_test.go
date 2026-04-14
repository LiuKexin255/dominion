package k8s

import "testing"

func TestDeploymentWorkload_Validate(t *testing.T) {
	tests := []struct {
		name     string
		workload DeploymentWorkload
		wantErr  bool
	}{
		{name: "valid", workload: DeploymentWorkload{ServiceName: "svc", EnvironmentName: "dev", App: "app", Image: "repo/app:v1", Replicas: 1, Ports: []*DeploymentPort{{Name: "http", Port: 8080}}}},
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
}
