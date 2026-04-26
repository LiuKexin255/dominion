package input

import "context"

// ReleaseAll sends release commands for all held mouse buttons to clean up
// state before disconnecting or stopping the helper.
func (m *Manager) ReleaseAll() error {
	m.mu.Lock()
	if m.stdin == nil {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	buttons := []Button{ButtonLeft, ButtonRight, ButtonMiddle}
	var lastErr error
	for _, button := range buttons {
		cmd := Command{
			Action: Action("release_" + string(button)),
			Button: button,
		}
		_, err := m.ExecuteCommand(context.Background(), cmd)
		if err != nil {
			lastErr = err
		}
	}
	return lastErr
}
