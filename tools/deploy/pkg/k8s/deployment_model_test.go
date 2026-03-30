package k8s

import "testing"

func TestDeploymentWorkloadValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   *DeploymentWorkload
		wantErr bool
	}{
		{
			name: "valid minimal workload",
			input: &DeploymentWorkload{
				Name:  "hello",
				App:   "grpc-hello-world",
				Desc:  "hello service",
				Image: "registry.local/hello:latest",
			},
		},
		{
			name: "missing name",
			input: &DeploymentWorkload{
				App:   "grpc-hello-world",
				Desc:  "hello service",
				Image: "registry.local/hello:latest",
			},
			wantErr: true,
		},
		{
			name: "missing app",
			input: &DeploymentWorkload{
				Name:  "hello",
				Desc:  "hello service",
				Image: "registry.local/hello:latest",
			},
			wantErr: true,
		},
		{
			name: "missing desc",
			input: &DeploymentWorkload{
				Name:  "hello",
				App:   "grpc-hello-world",
				Image: "registry.local/hello:latest",
			},
			wantErr: true,
		},
		{
			name: "missing image",
			input: &DeploymentWorkload{
				Name: "hello",
				App:  "grpc-hello-world",
				Desc: "hello service",
			},
			wantErr: true,
		},
		{
			name: "invalid replicas",
			input: &DeploymentWorkload{
				Name:     "hello",
				App:      "grpc-hello-world",
				Desc:     "hello service",
				Image:    "registry.local/hello:latest",
				Replicas: -1,
			},
			wantErr: true,
		},
		{
			name: "empty port name",
			input: &DeploymentWorkload{
				Name:  "hello",
				App:   "grpc-hello-world",
				Desc:  "hello service",
				Image: "registry.local/hello:latest",
				Ports: []DeploymentPort{{Name: "", Port: 8080}},
			},
			wantErr: true,
		},
		{
			name: "invalid port range",
			input: &DeploymentWorkload{
				Name:  "hello",
				App:   "grpc-hello-world",
				Desc:  "hello service",
				Image: "registry.local/hello:latest",
				Ports: []DeploymentPort{{Name: "http", Port: 70000}},
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
