//go:build !windows

package operation

import (
	"strings"
	"testing"

	"dominion/projects/game"
)

func TestMoveCursor_StubRejects(t *testing.T) {
	err := MoveCursor(100, 200)
	if err == nil {
		t.Fatal("MoveCursor() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("MoveCursor() error = %q, want to contain %q",
			err.Error(), "not supported")
	}
}

func TestExecuteClickAtCurrentPos_StubRejects(t *testing.T) {
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
			err := ExecuteClickAtCurrentPos(tt.action)
			if err == nil {
				t.Fatal("ExecuteClickAtCurrentPos() expected error, got nil")
			}
			if !strings.Contains(err.Error(), "not supported") {
				t.Errorf("ExecuteClickAtCurrentPos() error = %q, want to contain %q",
					err.Error(), "not supported")
			}
		})
	}
}
