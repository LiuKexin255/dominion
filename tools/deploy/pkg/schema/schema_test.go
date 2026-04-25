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
			raw: []byte(`name: grpc.dev
desc: 开发环境
type: dev
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
`),
		},
		{
			name: "valid deploy yaml with http match path",
			raw: []byte(`name: grpc.dev
desc: 开发环境
type: dev
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
    http:
      hostnames:
        - hello.example.com
      matches:
        - backend: grpc
          path:
            type: PathPrefix
            value: /v1
`),
		},
		{
			name: "deploy yaml rejects match without path",
			raw: []byte(`name: grpc.dev
desc: 开发环境
type: dev
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
    http:
      hostnames:
        - hello.example.com
      matches:
        - backend: grpc
`),
			wantErr: true,
		},
		{
			name: "valid infra deploy yaml",
			raw: []byte(`name: grpc.dev
desc: 开发环境
type: dev
services:
  - infra:
      resource: mongodb
      profile: development
      name: grpc-hello-world-mongo
      app: grpc-hello-world
      persistence:
        enabled: true
`),
		},
		{
			name: "invalid deploy yaml",
			raw: []byte(`name: grpc.dev
desc: 开发环境
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
`),
			wantErr: true,
		},
		{
			name: "invalid deploy yaml missing dot in name",
			raw: []byte(`name: grpcdev
desc: 开发环境
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
`),
			wantErr: true,
		},
		{
			name: "invalid deploy yaml uppercase name",
			raw: []byte(`name: Grpc.dev
desc: 开发环境
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
`),
			wantErr: true,
		},
		{
			name: "invalid deploy yaml too long name",
			raw: []byte(`name: grpctoolong.dev
desc: 开发环境
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
`),
			wantErr: true,
		},
		{
			name: "infra deploy yaml missing required fields",
			raw: []byte(`name: grpc.dev
desc: 开发环境
services:
  - infra:
      resource: mongo
      persistence:
        enabled: true
`),
			wantErr: true,
		},
		{
			name: "infra deploy yaml rejects unknown resource",
			raw: []byte(`name: grpc.dev
desc: 开发环境
services:
  - infra:
      resource: redis
      profile: development
      name: grpc-hello-world-redis
      app: grpc-hello-world
      persistence:
        enabled: true
`),
			wantErr: true,
		},
		{
			name: "infra and artifact are mutually exclusive",
			raw: []byte(`name: grpc.dev
desc: 开发环境
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
    infra:
      resource: mongo
      profile: development
      name: grpc-hello-world-mongo
      app: grpc-hello-world
      persistence:
        enabled: true
`),
			wantErr: true,
		},
		{
			name: "deploy yaml rejects tls fields",
			raw: []byte(`name: grpc.dev
desc: 开发环境
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
    tls:
      secret_name: grpc-hello-world-service-tls
`),
			wantErr: true,
		},
		{
			name: "valid deploy yaml with artifact env",
			raw: []byte(`name: grpc.dev
desc: 开发环境
type: dev
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
      env:
        FOO: bar
        BAZ: qux
`),
		},
		{
			name: "deploy yaml rejects non-string env value",
			raw: []byte(`name: grpc.dev
desc: 开发环境
type: dev
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
      env:
        FOO: 123
`),
			wantErr: true,
		},
		{
			name: "valid deploy yaml with run placeholder",
			raw: []byte(`name: game.{{run}}
desc: 动态环境
type: test
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
`),
		},
		{
			name: "invalid deploy yaml uppercase run placeholder",
			raw: []byte(`name: game.{{RUN}}
desc: 动态环境
type: test
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
`),
			wantErr: true,
		},
		{
			name: "invalid deploy yaml unknown placeholder",
			raw: []byte(`name: game.{{other}}
desc: 动态环境
type: test
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
`),
			wantErr: true,
		},
		{
			name: "invalid deploy yaml multiple dots with run placeholder",
			raw: []byte(`name: game.{{run}}.extra
desc: 动态环境
type: test
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
`),
			wantErr: true,
		},
		{
			name: "invalid deploy yaml multiple run placeholders",
			raw: []byte(`name: game.{{run}}{{run}}
desc: 动态环境
type: test
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
`),
			wantErr: true,
		},
		{
			name: "valid deploy yaml without env",
			raw: []byte(`name: grpc.dev
desc: 开发环境
type: dev
services:
  - artifact:
      path: //experimental/grpc_hello_world/service/service.yaml
      name: service
`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
    target: //experimental/grpc_hello_world/service:service_image
    tls: true
    ports:
      - name: grpc
        port: 50051
`),
		},
		{
			name: "valid service yaml without tls field",
			raw: []byte(`name: service
app: grpc-hello-world
desc: grpc hello world service
artifacts:
  - name: service
    target: //experimental/grpc_hello_world/service:service_image
    ports:
      - name: grpc
        port: 50051
`),
		},
		{
			name: "valid service yaml with stateful workload",
			raw: []byte(`name: service
app: grpc-hello-world
desc: grpc hello world service
kind: stateful
artifacts:
  - name: service
    target: //experimental/grpc_hello_world/service:service_image
`),
		},
		{
			name: "invalid service yaml",
			raw: []byte(`name: service
app: grpc-hello-world
desc: grpc hello world service
artifacts:
  - name: ""
    target: //experimental/grpc_hello_world/service:service_image
`),
			wantErr: true,
		},
		{
			name: "invalid service yaml workload kind",
			raw: []byte(`name: service
app: grpc-hello-world
desc: grpc hello world service
kind: invalid_value
artifacts:
  - name: service
    target: //experimental/grpc_hello_world/service:service_image
`),
			wantErr: true,
		},
		{
			name: "invalid service yaml tls is not boolean",
			raw: []byte(`name: service
app: grpc-hello-world
desc: grpc hello world service
artifacts:
  - name: service
    target: //experimental/grpc_hello_world/service:service_image
    tls: enabled
    ports:
      - name: grpc
        port: 50051
`),
			wantErr: true,
		},
		{
			name: "service yaml rejects runtime tls details",
			raw: []byte(`name: service
app: grpc-hello-world
desc: grpc hello world service
artifacts:
  - name: service
    target: //experimental/grpc_hello_world/service:service_image
    tls: true
    secret_name: grpc-hello-world-service-tls
    server_name: grpc-hello-world-service.default.svc.cluster.local
    ports:
      - name: grpc
        port: 50051
`),
			wantErr: true,
		},
		{
			name: "valid service yaml with top-level ports",
			raw: []byte(`name: service
app: grpc-hello-world
desc: grpc hello world service with ports
ports:
  - name: admin
    port: 9090
artifacts:
  - name: service
    target: //experimental/grpc_hello_world/service:service_image
    ports:
      - name: grpc
        port: 50051
`),
		},
		{
			name: "service yaml with invalid top-level port structure",
			raw: []byte(`name: service
app: grpc-hello-world
desc: grpc hello world service
ports:
  - name: admin
artifacts:
  - name: service
    target: //experimental/grpc_hello_world/service:service_image
`),
			wantErr: true,
		},
		{
			name: "service yaml with empty top-level ports array",
			raw: []byte(`name: service
app: grpc-hello-world
desc: grpc hello world service
ports: []
artifacts:
  - name: service
    target: //experimental/grpc_hello_world/service:service_image
`),
			wantErr: true,
		},
		{
			name: "service yaml without top-level ports is still valid",
			raw: []byte(`name: service
app: grpc-hello-world
desc: grpc hello world service
artifacts:
  - name: service
    target: //experimental/grpc_hello_world/service:service_image
`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			err := ValidateServiceYAML(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateServiceYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
