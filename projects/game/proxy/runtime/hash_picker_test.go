package runtime

import (
	"context"
	"errors"
	"testing"

	"dominion/common/gopkg/solver"
	"dominion/projects/game/proxy/domain"
)

func TestPickDeterministic(t *testing.T) {
	ctx := context.Background()
	picker := NewHashPicker()

	instances := []*solver.StatefulInstance{
		{Index: 0, Endpoints: []string{"host-0:5000"}},
		{Index: 1, Endpoints: []string{"host-1:5000"}},
		{Index: 2, Endpoints: []string{"host-2:5000"}},
	}

	// when: pick multiple times with same sessionID
	sessionID := "session-deterministic-123"
	first, err := picker.Pick(ctx, sessionID, instances)
	if err != nil {
		t.Fatalf("Pick() unexpected error: %v", err)
	}

	for i := 0; i < 10; i++ {
		got, err := picker.Pick(ctx, sessionID, instances)
		if err != nil {
			t.Fatalf("Pick() iteration %d unexpected error: %v", i, err)
		}
		// then: same sessionID always yields the same index
		if got != first {
			t.Fatalf("Pick() iteration %d = %d, want %d (deterministic)", i, got, first)
		}
	}
}

func TestPickDistribution(t *testing.T) {
	ctx := context.Background()
	picker := NewHashPicker()

	instances := []*solver.StatefulInstance{
		{Index: 0, Endpoints: []string{"host-0:5000"}},
		{Index: 1, Endpoints: []string{"host-1:5000"}},
		{Index: 2, Endpoints: []string{"host-2:5000"}},
	}

	// when: pick with different sessionIDs
	seen := map[int]bool{}
	for i := 0; i < 100; i++ {
		sessionID := "session-distro-" + string(rune('A'+i%26)) + string(rune(i/26+'a'))
		idx, err := picker.Pick(ctx, sessionID, instances)
		if err != nil {
			t.Fatalf("Pick() session %q unexpected error: %v", sessionID, err)
		}
		seen[idx] = true
	}

	// then: different sessionIDs should not all map to the same instance
	if len(seen) < 2 {
		t.Fatalf("Pick() distribution too narrow: only %d distinct instance(s) selected out of 3, want at least 2", len(seen))
	}
}

func TestPickEmptyInstances(t *testing.T) {
	ctx := context.Background()
	picker := NewHashPicker()

	// when: pick with empty instance list
	_, err := picker.Pick(ctx, "session-empty", nil)

	// then
	if err == nil {
		t.Fatalf("Pick() with empty instances expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNoAgentInstances) {
		t.Fatalf("Pick() error = %v, want ErrNoAgentInstances", err)
	}
}

func TestPickSingleInstance(t *testing.T) {
	ctx := context.Background()
	picker := NewHashPicker()

	instances := []*solver.StatefulInstance{
		{Index: 0, Endpoints: []string{"host-0:5000"}},
	}

	tests := []struct {
		name      string
		sessionID string
		want      int
	}{
		{name: "session A", sessionID: "session-a", want: 0},
		{name: "session B", sessionID: "session-b", want: 0},
		{name: "session C", sessionID: "session-c", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := picker.Pick(ctx, tt.sessionID, instances)

			// then
			if err != nil {
				t.Fatalf("Pick() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Pick() = %d, want %d", got, tt.want)
			}
		})
	}
}
