//go:build !windows

package operation

import (
	"testing"

	"dominion/projects/game"
)

func TestExecuteMouseAction_StubRejects(t *testing.T) {
	tests := []struct {
		name   string
		action game.AgentMouseAction
	}{
		{name: "left click", action: game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_CLICK},
		{name: "right click", action: game.AgentMouseAction_AGENT_MOUSE_ACTION_RIGHT_CLICK},
		{name: "left right press", action: game.AgentMouseAction_AGENT_MOUSE_ACTION_LEFT_RIGHT_PRESS},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ExecuteMouseAction(100, 200, tt.action)
			if err == nil {
				t.Fatal("ExecuteMouseAction() expected error, got nil")
			}
			if err.Error() != "not supported on this platform" {
				t.Errorf("ExecuteMouseAction() error = %q, want %q",
					err.Error(), "not supported on this platform")
			}
		})
	}
}
