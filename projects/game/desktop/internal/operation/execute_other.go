//go:build !windows

package operation

import "errors"

// ExecuteMouseClick is not supported on this platform.
func ExecuteMouseClick(screenX, screenY int32, button int32, clickType int32) error {
	return errors.New("mouse click not supported on this platform")
}
