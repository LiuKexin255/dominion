package solver

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDeployStatefulResolver_Resolve(t *testing.T) {
	tests := []struct {
		name            string
		response        *ServiceEndpointsInfo
		clientErr       error
		target          *Target
		want            []*StatefulInstance
		wantErr         error
		wantErrContains string
	}{
		{
			name: "success: returns stateful instances",
			response: &ServiceEndpointsInfo{
				IsStateful: true,
				StatefulInstances: []*StatefulInstance{
					{Index: 0, Endpoints: []string{"10.0.0.1:50051"}, Hostname: "svc-0"},
					{Index: 1, Endpoints: []string{"10.0.0.2:50051"}, Hostname: "svc-1"},
				},
			},
			target: &Target{App: "app-a", Service: "svc-b", PortSelector: NumericPort(50051)},
			want: []*StatefulInstance{
				{Index: 0, Endpoints: []string{"10.0.0.1:50051"}, Hostname: "svc-0"},
				{Index: 1, Endpoints: []string{"10.0.0.2:50051"}, Hostname: "svc-1"},
			},
		},
		{
			name: "not stateful returns ErrServiceNotStateful",
			response: &ServiceEndpointsInfo{
				IsStateful:        false,
				StatefulInstances: nil,
			},
			target:  &Target{App: "app-a", Service: "svc-b", PortSelector: NumericPort(50051)},
			wantErr: ErrServiceNotStateful,
		},
		{
			name: "stateful zero replicas returns empty not error",
			response: &ServiceEndpointsInfo{
				IsStateful:        true,
				StatefulInstances: nil,
			},
			target: &Target{App: "app-a", Service: "svc-b", PortSelector: NumericPort(50051)},
			want:   nil,
		},
		{
			name: "port filtering applied per instance",
			response: &ServiceEndpointsInfo{
				IsStateful: true,
				Ports:      map[string]int32{"grpc": 50051, "http": 8080},
				StatefulInstances: []*StatefulInstance{
					{Index: 0, Endpoints: []string{"10.0.0.1:50051", "10.0.0.1:8080"}, Hostname: "svc-0"},
					{Index: 1, Endpoints: []string{"10.0.0.2:50051", "10.0.0.2:8080"}, Hostname: "svc-1"},
				},
			},
			target: &Target{App: "app-a", Service: "svc-b", PortSelector: NumericPort(50051)},
			want: []*StatefulInstance{
				{Index: 0, Endpoints: []string{"10.0.0.1:50051"}, Hostname: "svc-0"},
				{Index: 1, Endpoints: []string{"10.0.0.2:50051"}, Hostname: "svc-1"},
			},
		},
		{
			name:            "client error passes through",
			clientErr:       errors.New("connection refused"),
			target:          &Target{App: "app-a", Service: "svc-b", PortSelector: NumericPort(50051)},
			wantErrContains: "connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			client := &fakeDeployClient{response: tt.response, err: tt.clientErr}
			resolver := &DeployStatefulResolver{client: client, scope: "dev", envName: "alpha"}

			// when
			got, err := resolver.Resolve(context.Background(), tt.target)

			// then
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if tt.wantErrContains != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErrContains)
				}
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
