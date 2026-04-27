package app

import (
	"context"
	"fmt"
	"time"

	agentruntime "dominion/projects/game/windows_agent/internal/runtime"
	"dominion/projects/game/windows_agent/internal/window"
)

// Connect establishes a gateway connection and updates the agent status.
// Callable from frontend via window.go.main.App.Connect(url).
func (a *App) Connect(connectURL string) error {
	if err := a.rt.Connect(context.Background(), connectURL); err != nil {
		a.setStatus(func(s *AgentStatus) {
			s.State = "Error"
			s.LastError = err.Error()
		})
		a.emitStatusChanged()
		a.emitEvent(EventErrorOccurred, err.Error())
		return err
	}

	sessionID, _ := agentruntime.ParseSessionURL(connectURL)
	a.setStatus(func(s *AgentStatus) {
		s.State = "Connected"
		s.SessionID = sessionID
		s.ConnectedAt = time.Now().UTC().Format(time.RFC3339)
		s.LastError = ""
	})
	a.emitStatusChanged()
	return nil
}

// Disconnect cleanly shuts down the runtime and resets the agent status.
// Callable from frontend via window.go.main.App.Disconnect().
func (a *App) Disconnect() error {
	err := a.rt.Disconnect()
	a.setStatus(func(s *AgentStatus) {
		s.State = "Disconnected"
		s.SessionID = ""
		s.BoundWindow = nil
		s.MediaSegCount = 0
		s.LastError = ""
		s.FFmpegRunning = false
		s.HelperRunning = false
		s.ConnectedAt = ""
	})
	a.emitStatusChanged()
	return err
}

// EnumerateWindows returns the list of visible top-level windows.
// Callable from frontend via window.go.main.App.EnumerateWindows().
func (a *App) EnumerateWindows() ([]window.WindowInfo, error) {
	windows, err := window.EnumerateWindows()
	if err != nil {
		return nil, fmt.Errorf("enumerate windows: %w", err)
	}
	a.emitEvent(EventWindowList, windows)
	return windows, nil
}

// BindWindow binds the agent to a specific window for capture.
// Callable from frontend via window.go.main.App.BindWindow(hwnd).
func (a *App) BindWindow(hwnd uintptr) error {
	var title string
	windows, _ := window.EnumerateWindows()
	for _, w := range windows {
		if w.HWND == hwnd {
			title = w.Title
			break
		}
	}

	if err := a.rt.BindWindow(hwnd); err != nil {
		a.setStatus(func(s *AgentStatus) {
			s.State = "Error"
			s.LastError = err.Error()
		})
		a.emitStatusChanged()
		a.emitEvent(EventErrorOccurred, err.Error())
		return err
	}

	a.setStatus(func(s *AgentStatus) {
		s.State = "Bound"
		s.BoundWindow = &WindowRef{HWND: hwnd, Title: title}
	})
	a.emitStatusChanged()
	return nil
}

// GetStatus returns a snapshot of the current agent status.
// Callable from frontend via window.go.main.App.GetStatus().
func (a *App) GetStatus() AgentStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}
