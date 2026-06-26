//go:build !windows

package operation

import (
	"errors"

	"dominion/projects/game"
)

// MoveCursor is not supported on non-Windows platforms.
func MoveCursor(screenX, screenY int32) error {
	return errors.New("not supported on this platform")
}

// ExecuteClickAtCurrentPos is not supported on non-Windows platforms.
func ExecuteClickAtCurrentPos(action game.AgentMouseAction) error {
	return errors.New("not supported on this platform")
}
