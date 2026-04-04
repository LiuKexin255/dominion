package schema

import "testing"

// TestValidateDeployYAML tests deploy YAML validation against the embedded schema.
func TestValidateDeployYAML(t *testing.T) {
	tests := []struct {
		name    string
		raw     []byte
		wantErr bool
	}{
		{
			name: "valid deploy yaml",
			raw: []byte(`template: deploy
app: grpc-hello-world
desc: 开发环境
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
`),
		},
		{
			name: "invalid deploy yaml",
			raw: []byte(`template: deploy
app: grpc-hello-world
desc: 开发环境
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
`),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BUILD_WORKSPACE_DIRECTORY", t.TempDir())
			t.Chdir(t.TempDir())

			err := ValidateDeployYAML(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDeployYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateServiceYAML tests service YAML validation against the embedded schema.
func TestValidateServiceYAML(t *testing.T) {
	tests := []struct {
		name    string
		raw     []byte
		wantErr bool
	}{
		{
			name: "valid service yaml",
			raw: []byte(`name: service
app: grpc-hello-world
desc: grpc hello world service
artifacts:
  - name: service
    type: deployment
    target: //experimental/grpc_hello_world/service:service_image
    ports:
      - name: grpc
        port: 50051
`),
		},
		{
			name: "invalid service yaml",
			raw: []byte(`name: service
app: grpc-hello-world
desc: grpc hello world service
artifacts:
  - name: service
    type: invalid
    target: //experimental/grpc_hello_world/service:service_image
`),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BUILD_WORKSPACE_DIRECTORY", t.TempDir())
			t.Chdir(t.TempDir())

			err := ValidateServiceYAML(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateServiceYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
