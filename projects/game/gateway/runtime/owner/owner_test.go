package owner

import (
	"context"
	"errors"
	"testing"

	"dominion/common/gopkg/solver"
)

// stubStatefulResolver implements solver.StatefulResolver for testing.
type stubStatefulResolver struct {
	instances []*solver.StatefulInstance
	err       error
}

func (s *stubStatefulResolver) Resolve(_ context.Context, _ *solver.Target) ([]*solver.StatefulInstance, error) {
	return s.instances, s.err
}

func TestDeployOwnerResolver_Resolve_Found(t *testing.T) {
	// given
	stub := &stubStatefulResolver{
		instances: []*solver.StatefulInstance{
			{Hostname: "game-gateway-0", Endpoints: []string{"http://10.0.0.5:8082"}},
			{Hostname: "game-gateway-1", Endpoints: []string{"http://10.0.0.6:8082"}},
		},
	}
	resolver := NewDeployOwnerResolver(stub, &solver.Target{})

	// when
	endpoint, err := resolver.Resolve(context.Background(), "game-gateway-0")

	// then
	if err != nil {
		t.Fatalf("Resolve() unexpected error: %v", err)
	}
	if endpoint != "http://10.0.0.5:8082" {
		t.Fatalf("Resolve() = %q, want %q", endpoint, "http://10.0.0.5:8082")
	}
}

func TestDeployOwnerResolver_Resolve_NotFound(t *testing.T) {
	// given
	stub := &stubStatefulResolver{
		instances: []*solver.StatefulInstance{
			{Hostname: "game-gateway-0", Endpoints: []string{"http://10.0.0.5:8082"}},
		},
	}
	resolver := NewDeployOwnerResolver(stub, &solver.Target{})

	// when
	_, err := resolver.Resolve(context.Background(), "game-gateway-99")

	// then
	if err == nil {
		t.Fatal("Resolve() expected error, got nil")
	}
	if !errors.Is(err, solver.ErrInstanceNotFound) {
		t.Fatalf("Resolve() error = %v, want %v", err, solver.ErrInstanceNotFound)
	}
}

func TestDeployOwnerResolver_Resolve_NoEndpoints(t *testing.T) {
	// given
	stub := &stubStatefulResolver{
		instances: []*solver.StatefulInstance{
			{Hostname: "game-gateway-0", Endpoints: []string{}},
		},
	}
	resolver := NewDeployOwnerResolver(stub, &solver.Target{})

	// when
	_, err := resolver.Resolve(context.Background(), "game-gateway-0")

	// then
	if err == nil {
		t.Fatal("Resolve() expected error, got nil")
	}
	if !errors.Is(err, solver.ErrInstanceNoReadyEndpoints) {
		t.Fatalf("Resolve() error = %v, want %v", err, solver.ErrInstanceNoReadyEndpoints)
	}
}

func TestNewResolver(t *testing.T) {
	t.Setenv("DOMINION_ENVIRONMENT", "dev.alpha")

	resolver, err := NewResolver()
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	if resolver == nil {
		t.Fatal("NewResolver() = nil, want resolver")
	}
}
