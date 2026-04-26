package gateway

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"dominion/common/gopkg/solver"
)

// fakeStatefulResolver implements solver.StatefulResolver for testing.
type fakeStatefulResolver struct {
	instances []*solver.StatefulInstance
	err       error
}

func (f *fakeStatefulResolver) Resolve(_ context.Context, _ *solver.Target) ([]*solver.StatefulInstance, error) {
	return f.instances, f.err
}

func TestDeployRegistry_PickRandom(t *testing.T) {
	tests := []struct {
		name string
		// given
		instances []*solver.StatefulInstance
		resolver  error
		wantIDs   []string
		err       error
	}{
		{
			name: "multiple ready instances returns one of them",
			instances: []*solver.StatefulInstance{
				{Index: 0, Endpoints: []string{"10.0.0.1:50051"}, Hostname: "gateway-0"},
				{Index: 1, Endpoints: []string{"10.0.0.2:50051"}, Hostname: "gateway-1"},
				{Index: 2, Endpoints: []string{"10.0.0.3:50051"}, Hostname: "gateway-2"},
			},
			wantIDs: []string{"gateway-0", "gateway-1", "gateway-2"},
		},
		{
			name: "single ready instance returns itself",
			instances: []*solver.StatefulInstance{
				{Index: 0, Endpoints: []string{"10.0.0.1:50051"}, Hostname: "gateway-0"},
			},
			wantIDs: []string{"gateway-0"},
		},
		{
			name: "filters out instances with empty endpoints",
			instances: []*solver.StatefulInstance{
				{Index: 0, Endpoints: []string{"10.0.0.1:50051"}, Hostname: "gateway-0"},
				{Index: 1, Endpoints: nil, Hostname: "gateway-1"},
				{Index: 2, Endpoints: []string{"10.0.0.3:50051"}, Hostname: "gateway-2"},
			},
			wantIDs: []string{"gateway-0", "gateway-2"},
		},
		{
			name: "all instances have no endpoints returns error",
			instances: []*solver.StatefulInstance{
				{Index: 0, Endpoints: nil, Hostname: "gateway-0"},
				{Index: 1, Endpoints: nil, Hostname: "gateway-1"},
			},
			err: ErrNoGatewayAvailable,
		},
		{
			name:      "empty instance list returns error",
			instances: nil,
			err:       ErrNoGatewayAvailable,
		},
		{
			name:     "resolver error passes through",
			resolver: errors.New("connection refused"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			fake := &fakeStatefulResolver{instances: tt.instances, err: tt.resolver}
			target, _ := solver.ParseTarget("game/gateway:50051")
			registry := NewDeployRegistry(fake, target, "gateway-%d-game.liukexin.com")

			// when
			got, err := registry.PickRandom(context.Background())

			// then
			if tt.resolver != nil {
				if err == nil {
					t.Fatalf("PickRandom() expected error containing %q", tt.resolver.Error())
				}
				if err.Error() != tt.resolver.Error() {
					t.Fatalf("PickRandom() error = %v, want %v", err, tt.resolver)
				}
				return
			}
			if !errors.Is(err, tt.err) {
				t.Fatalf("PickRandom() error = %v, want %v", err, tt.err)
			}
			if tt.err != nil {
				return
			}
			if !slices.Contains(tt.wantIDs, got.GatewayID) {
				t.Fatalf("PickRandom() = %q, want one of %v", got.GatewayID, tt.wantIDs)
			}
			wantHost := fmt.Sprintf("gateway-%d-game.liukexin.com", got.Index)
			if got.PublicHost != wantHost {
				t.Fatalf("PublicHost = %q, want %q", got.PublicHost, wantHost)
			}
		})
	}
}

func TestDeployRegistry_PickRandom_PublicHostFormat(t *testing.T) {
	// given
	instances := []*solver.StatefulInstance{
		{Index: 3, Endpoints: []string{"10.0.0.1:50051"}, Hostname: "gateway-3"},
	}
	fake := &fakeStatefulResolver{instances: instances}
	target, _ := solver.ParseTarget("game/gateway:50051")
	registry := NewDeployRegistry(fake, target, "gw-%d.example.com")

	// when
	got, err := registry.PickRandom(context.Background())

	// then
	if err != nil {
		t.Fatalf("PickRandom() unexpected error = %v", err)
	}
	if got.PublicHost != "gw-3.example.com" {
		t.Fatalf("PublicHost = %q, want %q", got.PublicHost, "gw-3.example.com")
	}
	if got.Index != 3 {
		t.Fatalf("Index = %d, want 3", got.Index)
	}
}

func TestDeployRegistry_PickRandomExcluding(t *testing.T) {
	tests := []struct {
		name      string
		excluding string
		// given
		instances []*solver.StatefulInstance
		resolver  error
		wantIDs   []string
		err       error
	}{
		{
			name: "multiple ready instances excludes the requested one",
			instances: []*solver.StatefulInstance{
				{Index: 0, Endpoints: []string{"10.0.0.1:50051"}, Hostname: "gateway-0"},
				{Index: 1, Endpoints: []string{"10.0.0.2:50051"}, Hostname: "gateway-1"},
				{Index: 2, Endpoints: []string{"10.0.0.3:50051"}, Hostname: "gateway-2"},
			},
			excluding: "gateway-1",
			wantIDs:   []string{"gateway-0", "gateway-2"},
		},
		{
			name: "single ready instance falls back to same instance",
			instances: []*solver.StatefulInstance{
				{Index: 0, Endpoints: []string{"10.0.0.1:50051"}, Hostname: "gateway-0"},
			},
			excluding: "gateway-0",
			wantIDs:   []string{"gateway-0"},
		},
		{
			name:      "empty instance list returns error",
			instances: nil,
			excluding: "gateway-0",
			err:       ErrNoGatewayAvailable,
		},
		{
			name: "all instances have no endpoints returns error",
			instances: []*solver.StatefulInstance{
				{Index: 0, Endpoints: nil, Hostname: "gateway-0"},
			},
			excluding: "gateway-0",
			err:       ErrNoGatewayAvailable,
		},
		{
			name: "excluding non-existent gateway returns from full list",
			instances: []*solver.StatefulInstance{
				{Index: 0, Endpoints: []string{"10.0.0.1:50051"}, Hostname: "gateway-0"},
				{Index: 1, Endpoints: []string{"10.0.0.2:50051"}, Hostname: "gateway-1"},
			},
			excluding: "gateway-9",
			wantIDs:   []string{"gateway-0", "gateway-1"},
		},
		{
			name:     "resolver error passes through",
			resolver: errors.New("connection refused"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			fake := &fakeStatefulResolver{instances: tt.instances, err: tt.resolver}
			target, _ := solver.ParseTarget("game/gateway:50051")
			registry := NewDeployRegistry(fake, target, "gateway-%d-game.liukexin.com")

			// when
			got, err := registry.PickRandomExcluding(context.Background(), tt.excluding)

			// then
			if tt.resolver != nil {
				if err == nil {
					t.Fatalf("PickRandomExcluding() expected error containing %q", tt.resolver.Error())
				}
				if err.Error() != tt.resolver.Error() {
					t.Fatalf("PickRandomExcluding() error = %v, want %v", err, tt.resolver)
				}
				return
			}
			if !errors.Is(err, tt.err) {
				t.Fatalf("PickRandomExcluding() error = %v, want %v", err, tt.err)
			}
			if tt.err != nil {
				return
			}
			if !slices.Contains(tt.wantIDs, got.GatewayID) {
				t.Fatalf("PickRandomExcluding() = %q, want one of %v", got.GatewayID, tt.wantIDs)
			}
		})
	}
}
