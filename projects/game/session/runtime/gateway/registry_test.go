package gateway

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestStaticRegistry_PickRandom(t *testing.T) {
	tests := []struct {
		name string
		// given
		assignments []*Assignment
		wantIDs     []string
		err         error
	}{
		{
			name:        "multiple gateways returns one of them",
			assignments: []*Assignment{{GatewayID: "gw-0", Index: 0, PublicHost: "host-0"}, {GatewayID: "gw-1", Index: 1, PublicHost: "host-1"}, {GatewayID: "gw-2", Index: 2, PublicHost: "host-2"}},
			wantIDs:     []string{"gw-0", "gw-1", "gw-2"},
		},
		{
			name:        "single gateway always returns itself",
			assignments: []*Assignment{{GatewayID: "gw-0", Index: 0, PublicHost: "host-0"}},
			wantIDs:     []string{"gw-0"},
		},
		{
			name:        "empty list returns error",
			assignments: nil,
			err:         ErrNoGatewayAvailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			registry := NewStaticRegistry(tt.assignments)

			// when
			got, err := registry.PickRandom(context.Background())

			// then
			if !errors.Is(err, tt.err) {
				t.Fatalf("PickRandom() error = %v, want %v", err, tt.err)
			}
			if tt.err != nil {
				return
			}
			if !slices.Contains(tt.wantIDs, got.GatewayID) {
				t.Fatalf("PickRandom() = %q, want one of %v", got.GatewayID, tt.wantIDs)
			}
		})
	}
}

func TestStaticRegistry_PickRandomExcluding(t *testing.T) {
	tests := []struct {
		name      string
		excluding string
		// given
		assignments []*Assignment
		wantIDs     []string
		err         error
	}{
		{
			name:        "multiple gateways excludes the requested one",
			assignments: []*Assignment{{GatewayID: "gw-0", Index: 0, PublicHost: "host-0"}, {GatewayID: "gw-1", Index: 1, PublicHost: "host-1"}, {GatewayID: "gw-2", Index: 2, PublicHost: "host-2"}},
			excluding:   "gw-1",
			wantIDs:     []string{"gw-0", "gw-2"},
		},
		{
			name:        "single gateway degrades to the same gateway",
			assignments: []*Assignment{{GatewayID: "gw-0", Index: 0, PublicHost: "host-0"}},
			excluding:   "gw-0",
			wantIDs:     []string{"gw-0"},
		},
		{
			name:        "empty list returns error",
			assignments: nil,
			excluding:   "gw-0",
			err:         ErrNoGatewayAvailable,
		},
		{
			name:        "missing gateway returns from full list",
			assignments: []*Assignment{{GatewayID: "gw-0", Index: 0, PublicHost: "host-0"}, {GatewayID: "gw-1", Index: 1, PublicHost: "host-1"}},
			excluding:   "gw-9",
			wantIDs:     []string{"gw-0", "gw-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			registry := NewStaticRegistry(tt.assignments)

			// when
			got, err := registry.PickRandomExcluding(context.Background(), tt.excluding)

			// then
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

func TestStaticRegistry_ConstructorCopiesInput(t *testing.T) {
	// given
	assignments := []*Assignment{{GatewayID: "gw-0", Index: 0, PublicHost: "host-0"}, {GatewayID: "gw-1", Index: 1, PublicHost: "host-1"}}
	registry := NewStaticRegistry(assignments)

	// when
	assignments[0].GatewayID = "mutated"
	got, err := registry.PickRandomExcluding(context.Background(), "gw-1")

	// then
	if err != nil {
		t.Fatalf("PickRandomExcluding() unexpected error = %v", err)
	}
	if got.GatewayID == "mutated" {
		t.Fatalf("registry retained caller slice mutation")
	}
}
