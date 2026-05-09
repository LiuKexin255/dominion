package solver

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"dominion/common/gopkg/logs"
)

// fakeDeployClient implements DeployEndpointClient for testing.
type fakeDeployClient struct {
	response *ServiceEndpointsInfo
	err      error
}

func (c *fakeDeployClient) GetServiceEndpoints(_ context.Context, _ string) (*ServiceEndpointsInfo, error) {
	return c.response, c.err
}

// testEnvLookup returns an env lookup function backed by the given map.
func testEnvLookup(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func TestNewDeployResolver(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{
			name: "success",
			env: map[string]string{
				dominionEnvironmentEnvKey: "dev.alpha",
				podNamespaceEnvKey:        "ns-test",
			},
		},
		{
			name: "missing dominion environment",
			env: map[string]string{
				podNamespaceEnvKey: "ns-test",
			},
			wantErr: true,
		},
		{
			name: "invalid format without dot",
			env: map[string]string{
				dominionEnvironmentEnvKey: "invalid",
				podNamespaceEnvKey:        "ns-test",
			},
			wantErr: true,
		},
		{
			name: "empty scope",
			env: map[string]string{
				dominionEnvironmentEnvKey: ".alpha",
				podNamespaceEnvKey:        "ns-test",
			},
			wantErr: true,
		},
		{
			name: "empty env name",
			env: map[string]string{
				dominionEnvironmentEnvKey: "dev.",
				podNamespaceEnvKey:        "ns-test",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLookupEnv := lookupEnv
			lookupEnv = testEnvLookup(tt.env)
			t.Cleanup(func() {
				lookupEnv = originalLookupEnv
			})

			// when
			_, err := NewDeployResolver()

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDeployResolver_Resolve(t *testing.T) {
	tests := []struct {
		name      string
		response  *ServiceEndpointsInfo
		clientErr error
		target    *Target
		want      []string
		wantErr   bool
	}{
		{
			name: "numeric port filters matching endpoints",
			response: &ServiceEndpointsInfo{
				Endpoints: []string{"10.0.0.1:50051", "10.0.0.1:8080"},
			},
			target: &Target{App: "app-a", Service: "svc-b", PortSelector: NumericPort(50051)},
			want:   []string{"10.0.0.1:50051"},
		},
		{
			name: "numeric port with no match returns nil",
			response: &ServiceEndpointsInfo{
				Endpoints: []string{"10.0.0.1:50051"},
			},
			target: &Target{App: "app-a", Service: "svc-b", PortSelector: NumericPort(9090)},
			want:   nil,
		},
		{
			name: "named port resolves via ports map",
			response: &ServiceEndpointsInfo{
				Endpoints: []string{"10.0.0.1:50051", "10.0.0.1:8080"},
				Ports:     map[string]int32{"grpc": 50051, "http": 8080},
			},
			target: &Target{App: "app-a", Service: "svc-b", PortSelector: NamedPort("grpc")},
			want:   []string{"10.0.0.1:50051"},
		},
		{
			name: "named port deduplicates hosts",
			response: &ServiceEndpointsInfo{
				Endpoints: []string{"10.0.0.1:50051", "10.0.0.1:8080", "10.0.0.2:50051"},
				Ports:     map[string]int32{"grpc": 50051},
			},
			target: &Target{App: "app-a", Service: "svc-b", PortSelector: NamedPort("grpc")},
			want:   []string{"10.0.0.1:50051", "10.0.0.2:50051"},
		},
		{
			name: "named port not found returns error",
			response: &ServiceEndpointsInfo{
				Endpoints: []string{"10.0.0.1:50051"},
				Ports:     map[string]int32{"grpc": 50051},
			},
			target:  &Target{App: "app-a", Service: "svc-b", PortSelector: NamedPort("http")},
			wantErr: true,
		},
		{
			name: "nil ports map with named port returns error",
			response: &ServiceEndpointsInfo{
				Endpoints: []string{"10.0.0.1:50051"},
			},
			target:  &Target{App: "app-a", Service: "svc-b", PortSelector: NamedPort("grpc")},
			wantErr: true,
		},
		{
			name:   "nil response returns nil",
			target: &Target{App: "app-a", Service: "svc-b", PortSelector: NumericPort(50051)},
			want:   nil,
		},
		{
			name: "empty response returns nil",
			response: &ServiceEndpointsInfo{
				Endpoints: []string{},
			},
			target: &Target{App: "app-a", Service: "svc-b", PortSelector: NumericPort(50051)},
			want:   nil,
		},
		{
			name:      "deploy error",
			clientErr: errors.New("connection refused"),
			target:    &Target{App: "app-a", Service: "svc-b", PortSelector: NumericPort(50051)},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			client := &fakeDeployClient{response: tt.response, err: tt.clientErr}
			resolver := &DeployResolver{client: client, scope: "dev", envName: "alpha"}

			// when
			got, err := resolver.Resolve(context.Background(), tt.target)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Resolve(%#v) expected error", tt.target)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve(%#v) unexpected error: %v", tt.target, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Resolve(%#v) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}

func TestDeployResolver_Resolve_WarnOnNoEndpoints(t *testing.T) {
	tests := []struct {
		name      string
		response  *ServiceEndpointsInfo
		target    *Target
		wantLog   bool
		wantCount string
	}{
		{
			name:      "nil info logs warning with endpoint_count 0",
			response:  nil,
			target:    &Target{App: "app-a", Service: "svc-b", PortSelector: NumericPort(50051)},
			wantLog:   true,
			wantCount: "endpoint_count=0",
		},
		{
			name: "empty endpoints after filter logs warning",
			response: &ServiceEndpointsInfo{
				Endpoints: []string{"10.0.0.1:8080"},
			},
			target:    &Target{App: "app-a", Service: "svc-b", PortSelector: NumericPort(50051)},
			wantLog:   true,
			wantCount: "endpoint_count=0",
		},
		{
			name: "matching endpoints does not log warning",
			response: &ServiceEndpointsInfo{
				Endpoints: []string{"10.0.0.1:50051"},
			},
			target:  &Target{App: "app-a", Service: "svc-b", PortSelector: NumericPort(50051)},
			wantLog: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
			ctx := logs.WithLogger(context.Background(), logger)

			client := &fakeDeployClient{response: tt.response}
			resolver := &DeployResolver{client: client, scope: "dev", envName: "alpha"}

			// when
			_, err := resolver.Resolve(ctx, tt.target)

			// then
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			output := buf.String()
			if tt.wantLog {
				if !strings.Contains(output, "deploy resolver returned no endpoints") {
					t.Fatalf("log output = %q, want warning message", output)
				}
				if !strings.Contains(output, tt.wantCount) {
					t.Fatalf("log output = %q, want %q", output, tt.wantCount)
				}
				if !strings.Contains(output, "app=app-a") {
					t.Fatalf("log output = %q, want app=app-a", output)
				}
				if !strings.Contains(output, "service=svc-b") {
					t.Fatalf("log output = %q, want service=svc-b", output)
				}
			} else {
				if strings.Contains(output, "deploy resolver returned no endpoints") {
					t.Fatalf("unexpected warning log: %q", output)
				}
			}
		})
	}
}
