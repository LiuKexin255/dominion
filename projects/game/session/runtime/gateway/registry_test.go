package gateway

import (
	"errors"
	"slices"
	"testing"
)

func TestStaticRegistry_PickRandom(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want []string
		err  error
	}{
		{
			name: "multiple gateways returns one of them",
			ids:  []string{"game-gateway-0", "game-gateway-1", "game-gateway-2"},
			want: []string{"game-gateway-0", "game-gateway-1", "game-gateway-2"},
		},
		{
			name: "single gateway always returns itself",
			ids:  []string{"game-gateway-0"},
			want: []string{"game-gateway-0"},
		},
		{
			name: "empty list returns error",
			ids:  nil,
			err:  ErrNoGatewayAvailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			registry := NewStaticRegistry(tt.ids)

			// when
			got, err := registry.PickRandom()

			// then
			if !errors.Is(err, tt.err) {
				t.Fatalf("PickRandom() error = %v, want %v", err, tt.err)
			}
			if tt.err != nil {
				return
			}
			if !slices.Contains(tt.want, got) {
				t.Fatalf("PickRandom() = %q, want one of %v", got, tt.want)
			}
		})
	}
}

func TestStaticRegistry_PickRandomExcluding(t *testing.T) {
	tests := []struct {
		name      string
		ids       []string
		excluding string
		want      []string
		err       error
	}{
		{
			name:      "multiple gateways excludes the requested one",
			ids:       []string{"game-gateway-0", "game-gateway-1", "game-gateway-2"},
			excluding: "game-gateway-1",
			want:      []string{"game-gateway-0", "game-gateway-2"},
		},
		{
			name:      "single gateway degrades to the same gateway",
			ids:       []string{"game-gateway-0"},
			excluding: "game-gateway-0",
			want:      []string{"game-gateway-0"},
		},
		{
			name:      "empty list returns error",
			ids:       nil,
			excluding: "game-gateway-0",
			err:       ErrNoGatewayAvailable,
		},
		{
			name:      "missing gateway returns from full list",
			ids:       []string{"game-gateway-0", "game-gateway-1"},
			excluding: "game-gateway-9",
			want:      []string{"game-gateway-0", "game-gateway-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			registry := NewStaticRegistry(tt.ids)

			// when
			got, err := registry.PickRandomExcluding(tt.excluding)

			// then
			if !errors.Is(err, tt.err) {
				t.Fatalf("PickRandomExcluding() error = %v, want %v", err, tt.err)
			}
			if tt.err != nil {
				return
			}
			if !slices.Contains(tt.want, got) {
				t.Fatalf("PickRandomExcluding() = %q, want one of %v", got, tt.want)
			}
			if !slices.Contains(tt.want, tt.excluding) && got == tt.excluding {
				t.Fatalf("PickRandomExcluding() = %q, want excluded gateway %q to be avoided", got, tt.excluding)
			}
		})
	}
}

func TestStaticRegistry_ConstructorCopiesInput(t *testing.T) {
	// given
	ids := []string{"game-gateway-0", "game-gateway-1"}
	registry := NewStaticRegistry(ids)

	// when
	ids[0] = "mutated"
	got, err := registry.PickRandomExcluding("mutated")

	// then
	if err != nil {
		t.Fatalf("PickRandomExcluding() unexpected error = %v", err)
	}
	if got == "mutated" {
		t.Fatalf("registry retained caller slice mutation")
	}
}
