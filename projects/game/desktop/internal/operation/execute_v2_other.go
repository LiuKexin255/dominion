//go:build !windows

package operation

import (
	"errors"

	"dominion/projects/game"
)

// ExecuteMouseAction is not supported on non-Windows platforms.
func ExecuteMouseAction(screenX, screenY int32, action game.AgentMouseAction) error {
	return errors.New("not supported on this platform")
}
