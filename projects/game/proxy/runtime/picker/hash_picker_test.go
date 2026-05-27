package picker

import (
	"context"
	"errors"
	"testing"

	"dominion/projects/game/proxy/domain"
	"dominion/projects/game/proxy/runtime/agentclient"
)

func TestPickDeterministic(t *testing.T) {
	ctx := context.Background()
	picker := NewHashPicker()

	clients := []agentclient.ClientRef{
		{OwnerIndex: 0, Owner: "host-0:5000"},
		{OwnerIndex: 1, Owner: "host-1:5000"},
		{OwnerIndex: 2, Owner: "host-2:5000"},
	}

	// when: pick multiple times with same sessionID
	sessionID := "session-deterministic-123"
	first, err := picker.Pick(ctx, sessionID, clients)
	if err != nil {
		t.Fatalf("Pick() unexpected error: %v", err)
	}

	for i := 0; i < 10; i++ {
		got, err := picker.Pick(ctx, sessionID, clients)
		if err != nil {
			t.Fatalf("Pick() iteration %d unexpected error: %v", i, err)
		}
		// then: same sessionID always yields the same client
		if got.OwnerIndex != first.OwnerIndex {
			t.Fatalf("Pick() iteration %d = %d, want %d (deterministic)", i, got.OwnerIndex, first.OwnerIndex)
		}
	}
}

func TestPickDistribution(t *testing.T) {
	ctx := context.Background()
	picker := NewHashPicker()

	clients := []agentclient.ClientRef{
		{OwnerIndex: 0, Owner: "host-0:5000"},
		{OwnerIndex: 1, Owner: "host-1:5000"},
		{OwnerIndex: 2, Owner: "host-2:5000"},
	}

	// when: pick with different sessionIDs
	seen := map[int]bool{}
	for i := 0; i < 100; i++ {
		sessionID := "session-distro-" + string(rune('A'+i%26)) + string(rune(i/26+'a'))
		got, err := picker.Pick(ctx, sessionID, clients)
		if err != nil {
			t.Fatalf("Pick() session %q unexpected error: %v", sessionID, err)
		}
		seen[got.OwnerIndex] = true
	}

	// then: different sessionIDs should not all map to the same instance
	if len(seen) < 2 {
		t.Fatalf("Pick() distribution too narrow: only %d distinct instance(s) selected out of 3, want at least 2", len(seen))
	}
}

func TestPickEmptyClients(t *testing.T) {
	ctx := context.Background()
	picker := NewHashPicker()

	// when: pick with empty client list
	_, err := picker.Pick(ctx, "session-empty", nil)

	// then
	if err == nil {
		t.Fatalf("Pick() with empty clients expected error, got nil")
	}
	if !errors.Is(err, domain.ErrNoAgentInstances) {
		t.Fatalf("Pick() error = %v, want ErrNoAgentInstances", err)
	}
}

func TestPickSingleClient(t *testing.T) {
	ctx := context.Background()
	picker := NewHashPicker()

	clients := []agentclient.ClientRef{
		{OwnerIndex: 0, Owner: "host-0:5000"},
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
			got, err := picker.Pick(ctx, tt.sessionID, clients)

			// then
			if err != nil {
				t.Fatalf("Pick() unexpected error: %v", err)
			}
			if got.OwnerIndex != tt.want {
				t.Fatalf("Pick() = %d, want %d", got.OwnerIndex, tt.want)
			}
		})
	}
}
