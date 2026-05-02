package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	agentruntime "dominion/projects/game/windows_agent/internal/runtime"
	"dominion/projects/game/windows_agent/internal/window"

	sessionpb "dominion/projects/game/session"
)

const logModuleApp = "app"

// Connect establishes a gateway connection and updates the agent status.
// Callable from frontend via window.go.main.App.Connect(url).
func (a *App) Connect(connectURL string) error {
	if err := a.rt.Connect(context.Background(), connectURL); err != nil {
		a.setStatus(func(s *AgentStatus) {
			s.State = "Disconnected"
			s.LastError = "connect failed"
		})
		a.emitStatusChanged()
		a.emitEvent(EventErrorOccurred, err.Error())
		a.log("error", "connect failed", map[string]string{"gateway": sanitizeURL(connectURL), "error": err.Error()})
		return err
	}

	sessionID, _ := agentruntime.ParseSessionURL(connectURL)
	startTime := time.Now().UTC().Format(time.RFC3339)
	a.setStatus(func(s *AgentStatus) {
		s.State = "Connected"
		s.StreamingState = "Idle"
		s.SessionID = sessionID
		s.SessionName = sessionNameFromID(sessionID)
		s.SessionType = ""
		s.GatewayID = gatewayHost(connectURL)
		s.ConnectedAt = startTime
		s.LastError = ""
		s.StreamingLastError = ""
	})
	a.emitStatusChanged()
	a.log("info", "connected", map[string]string{"gateway": sanitizeURL(connectURL), "session": sessionNameFromID(sessionID)})
	return nil
}

// Disconnect cleanly shuts down the runtime and resets the agent status.
// Callable from frontend via window.go.main.App.Disconnect().
func (a *App) Disconnect() error {
	err := a.rt.Disconnect()
	a.sessionMu.Lock()
	a.currentSession = nil
	a.sessionMu.Unlock()
	a.setStatus(func(s *AgentStatus) {
		s.State = "Disconnected"
		s.StreamingState = "Idle"
		s.SessionID = ""
		s.SessionName = ""
		s.SessionType = ""
		s.GatewayID = ""
		s.BoundWindow = nil
		s.MediaSegCount = 0
		s.LastError = ""
		s.StreamingLastError = ""
		s.FFmpegRunning = false
		s.HelperRunning = false
		s.ConnectedAt = ""
		s.StreamingStartedAt = ""
	})
	a.emitStatusChanged()
	a.log("info", "disconnected", nil)
	return err
}

// ListSessions returns active remote sessions from the session service.
// Callable from frontend via window.go.main.App.ListSessions().
func (a *App) ListSessions() ([]Session, error) {
	sessions, err := a.sc.ListSessions(context.Background())
	if err != nil {
		a.log("error", "list sessions failed", map[string]string{"error": err.Error()})
		a.setSessionServiceState("error", err.Error())
		return nil, err
	}
	a.setSessionServiceState("ok", "")
	result := make([]Session, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, convertSession(session))
	}
	if len(result) == 0 {
		return nil, nil
	}
	a.log("info", "listed sessions", map[string]string{"count": fmt.Sprintf("%d", len(result))})
	return result, nil
}

// CreateSession creates a remote session of the requested type.
// Callable from frontend via window.go.main.App.CreateSession(type).
func (a *App) CreateSession(sessionType string) (*Session, error) {
	session, err := a.sc.CreateSession(context.Background(), sessionType)
	if err != nil {
		a.log("error", "create session failed", map[string]string{"type": sessionType, "error": err.Error()})
		a.setSessionServiceState("error", err.Error())
		return nil, err
	}
	a.setSessionServiceState("ok", "")
	result := convertSession(session)
	a.log("info", "created session", map[string]string{"name": result.Name, "type": result.Type, "gateway": sanitizeURL(result.AgentConnectURL)})
	return &result, nil
}

// ConnectSession connects to a remote session, reconnecting it once if needed.
// Callable from frontend via window.go.main.App.ConnectSession(session).
func (a *App) ConnectSession(session Session) error {
	activeSession := session
	if err := a.rt.Connect(context.Background(), session.AgentConnectURL); err != nil {
		a.log("info", "session connect failed; reconnecting", map[string]string{"name": session.Name, "gateway": sanitizeURL(session.AgentConnectURL)})
		newSession, reconnectErr := a.sc.ReconnectSession(context.Background(), session.Name)
		if reconnectErr != nil {
			err = fmt.Errorf("connect failed (%w), reconnect failed (%w)", err, reconnectErr)
			a.setStatus(func(s *AgentStatus) {
				s.State = "Disconnected"
				s.LastError = "session reconnect failed"
			})
			a.emitStatusChanged()
			a.emitEvent(EventErrorOccurred, "session reconnect failed")
			a.log("error", "session reconnect failed", map[string]string{"name": session.Name, "error": err.Error()})
			return err
		}
		activeSession = convertSession(newSession)
		if retryErr := a.rt.Connect(context.Background(), activeSession.AgentConnectURL); retryErr != nil {
			a.setStatus(func(s *AgentStatus) {
				s.State = "Disconnected"
				s.LastError = "session reconnect retry failed"
			})
			a.emitStatusChanged()
			a.emitEvent(EventErrorOccurred, retryErr.Error())
			a.log("error", "session reconnect retry failed", map[string]string{"name": activeSession.Name, "gateway": sanitizeURL(activeSession.AgentConnectURL), "error": retryErr.Error()})
			return retryErr
		}
	}

	a.sessionMu.Lock()
	a.currentSession = &activeSession
	a.sessionMu.Unlock()

	sessionID, _ := agentruntime.ParseSessionURL(activeSession.AgentConnectURL)
	startTime := time.Now().UTC().Format(time.RFC3339)
		a.setStatus(func(s *AgentStatus) {
			s.State = "Connected"
			s.StreamingState = "Idle"
			s.SessionID = sessionID
			s.SessionName = activeSession.Name
			s.SessionType = activeSession.Type
			s.GatewayID = activeSession.GatewayID
			s.ConnectedAt = startTime
			s.LastError = ""
			s.StreamingLastError = ""
		})
	a.emitStatusChanged()
	a.log("info", "connected to session", map[string]string{"name": activeSession.Name, "gateway": sanitizeURL(activeSession.AgentConnectURL)})
	return nil
}

// DeleteSession deletes a remote session, safely disconnecting if it is current.
// Callable from frontend via window.go.main.App.DeleteSession(name).
func (a *App) DeleteSession(name string) error {
	if a.isCurrentSession(name) {
		status := a.GetStatus()
		if status.StreamingState == "Streaming" {
			if err := a.rt.StopCapture(); err != nil {
				a.log("error", "stop capture before delete failed", map[string]string{"name": name, "error": err.Error()})
				return err
			}
		}
		if err := a.Disconnect(); err != nil {
			a.log("error", "disconnect before delete failed", map[string]string{"name": name, "error": err.Error()})
			return err
		}
	}
	if err := a.sc.DeleteSession(context.Background(), name); err != nil {
		a.log("error", "delete session failed", map[string]string{"name": name, "error": err.Error()})
		a.setSessionServiceState("error", err.Error())
		return err
	}
	a.setSessionServiceState("ok", "")
	a.log("info", "deleted session", map[string]string{"name": name})
	return nil
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
	detail := WindowDetail{WindowRef: WindowRef{HWND: hwnd}}
	windows, _ := window.EnumerateWindows()
	for _, w := range windows {
		if w.HWND == hwnd {
			detail = convertWindowDetail(w)
			break
		}
	}

	if err := a.rt.BindWindow(hwnd); err != nil {
		a.setStatus(func(s *AgentStatus) {
			s.LastError = "bind window failed"
		})
		a.emitStatusChanged()
		a.emitEvent(EventErrorOccurred, err.Error())
		a.log("error", "bind window failed", map[string]string{"hwnd": fmt.Sprintf("%d", hwnd), "error": err.Error()})
		return err
	}

	a.setStatus(func(s *AgentStatus) {
		s.State = "Connected"
		s.BoundWindow = &detail
		s.LastError = ""
	})
	a.emitStatusChanged()
	a.log("info", "bound window", map[string]string{"hwnd": fmt.Sprintf("%d", hwnd), "title": detail.Title})
	return nil
}

// ClearWindow clears the current window binding.
// Callable from frontend via window.go.main.App.ClearWindow().
func (a *App) ClearWindow() error {
	if err := a.rt.ClearWindow(); err != nil {
		a.setStatus(func(s *AgentStatus) {
			s.LastError = "clear window failed"
		})
		a.emitStatusChanged()
		a.emitEvent(EventErrorOccurred, err.Error())
		a.log("error", "clear window failed", map[string]string{"error": err.Error()})
		return err
	}
	a.setStatus(func(s *AgentStatus) {
		s.BoundWindow = nil
		s.StreamingStartedAt = ""
		s.LastError = ""
	})
	a.emitStatusChanged()
	a.log("info", "cleared window", nil)
	return nil
}

// StartCapture starts capture for the bound window.
// Callable from frontend via window.go.main.App.StartCapture().
func (a *App) StartCapture() error {
	status := a.GetStatus()
	if status.State != "Connected" && status.State != "Bound" {
		return a.capturePreconditionError(fmt.Sprintf("cannot start capture in state %s", status.State))
	}
	if status.BoundWindow == nil {
		return a.capturePreconditionError("cannot start capture without a bound window")
	}
	if err := a.rt.StartCapture(context.Background()); err != nil {
		a.setStatus(func(s *AgentStatus) {
			s.StreamingState = "Error"
			s.StreamingLastError = "start capture failed"
		})
		a.emitStatusChanged()
		a.emitEvent(EventErrorOccurred, err.Error())
		a.log("error", "start capture failed", map[string]string{"error": err.Error()})
		return err
	}
	startTime := time.Now().UTC().Format(time.RFC3339)
	a.setStatus(func(s *AgentStatus) {
		s.StreamingState = "Streaming"
		s.StreamingStartedAt = startTime
		s.FFmpegRunning = true
		s.HelperRunning = true
		s.StreamingLastError = ""
	})
	a.emitStatusChanged()
	a.log("info", "started capture", nil)
	return nil
}

// StopCapture stops active capture while keeping the session/window binding.
// Callable from frontend via window.go.main.App.StopCapture().
func (a *App) StopCapture() error {
	if err := a.rt.StopCapture(); err != nil {
		a.setStatus(func(s *AgentStatus) {
			s.StreamingState = "Error"
			s.StreamingLastError = "stop capture failed"
		})
		a.emitStatusChanged()
		a.emitEvent(EventErrorOccurred, err.Error())
		a.log("error", "stop capture failed", map[string]string{"error": err.Error()})
		return err
	}
	a.setStatus(func(s *AgentStatus) {
		s.StreamingState = "Idle"
		s.StreamingStartedAt = ""
		s.FFmpegRunning = false
		s.HelperRunning = false
		s.StreamingLastError = ""
	})
	a.emitStatusChanged()
	a.log("info", "stopped capture", nil)
	return nil
}

// TakeScreenshot fetches the latest game snapshot through the session gateway.
// Callable from frontend via window.go.main.App.TakeScreenshot().
func (a *App) TakeScreenshot() (ScreenshotResult, error) {
	a.sessionMu.RLock()
	session := a.currentSession
	a.sessionMu.RUnlock()
	if session == nil {
		err := fmt.Errorf("no active session")
		return ScreenshotResult{Error: err.Error()}, err
	}
	parsed, err := url.Parse(session.AgentConnectURL)
	if err != nil {
		return ScreenshotResult{SessionName: session.Name, GatewayID: session.GatewayID, Error: err.Error()}, err
	}
	snapshotURL := fmt.Sprintf("https://%s/v1/%s/game/snapshot", parsed.Host, session.Name)
	resp, err := http.Get(snapshotURL)
	if err != nil {
		return ScreenshotResult{SessionName: session.Name, GatewayID: session.GatewayID, Error: err.Error()}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("snapshot HTTP %d", resp.StatusCode)
		return ScreenshotResult{SessionName: session.Name, GatewayID: session.GatewayID, Error: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, body)}, err
	}
	snapshot := new(snapshotResponse)
	if err := json.NewDecoder(resp.Body).Decode(snapshot); err != nil {
		return ScreenshotResult{SessionName: session.Name, GatewayID: session.GatewayID, Error: err.Error()}, err
	}
	result := ScreenshotResult{
		ImageURL:    dataURL(snapshot.MimeType, snapshot.Image),
		MimeType:    snapshot.MimeType,
		SnapshotID:  snapshot.SnapshotID,
		CaptureTime: snapshot.CaptureTime,
		SessionName: session.Name,
		GatewayID:   session.GatewayID,
	}
	a.log("info", "took screenshot", map[string]string{"name": session.Name, "gateway": sanitizeURL(session.AgentConnectURL), "snapshot": snapshot.SnapshotID})
	return result, nil
}

// GetStatus returns a snapshot of the current agent status.
// Callable from frontend via window.go.main.App.GetStatus().
func (a *App) GetStatus() AgentStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

type snapshotResponse struct {
	SnapshotID  string `json:"snapshot_id"`
	MimeType    string `json:"mime_type"`
	Image       string `json:"image"`
	CaptureTime string `json:"capture_time"`
}

func (a *App) capturePreconditionError(message string) error {
	err := errors.New(message)
	a.setStatus(func(s *AgentStatus) {
		s.LastError = "start capture precondition failed"
	})
	a.emitStatusChanged()
	a.emitEvent(EventErrorOccurred, err.Error())
	a.log("error", "start capture precondition failed", map[string]string{"reason": message})
	return err
}

func (a *App) isCurrentSession(name string) bool {
	a.sessionMu.RLock()
	defer a.sessionMu.RUnlock()
	return a.currentSession != nil && a.currentSession.Name == name
}

func (a *App) log(level, message string, fields map[string]string) {
	a.EmitStructuredLog(LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		Module:    logModuleApp,
		Message:   message,
		Fields:    fields,
	})
}

func convertSession(pb *sessionpb.Session) Session {
	s := Session{
		Name:                pb.GetName(),
		Type:                pb.GetType().String(),
		Status:              pb.GetStatus().String(),
		GatewayID:           pb.GetGatewayId(),
		AgentConnectURL:     pb.GetAgentConnectUrl(),
		ReconnectGeneration: fmt.Sprintf("%d", pb.GetReconnectGeneration()),
		LastError:           pb.GetLastError(),
	}
	if t := pb.GetCreateTime(); t != nil {
		s.CreateTime = t.AsTime().UTC().Format(time.RFC3339)
	}
	if t := pb.GetUpdateTime(); t != nil {
		s.UpdateTime = t.AsTime().UTC().Format(time.RFC3339)
	}
	return s
}

func convertWindowDetail(info window.WindowInfo) WindowDetail {
	return WindowDetail{
		WindowRef: WindowRef{HWND: info.HWND, Title: info.Title},
		ClassName: info.ClassName,
		ProcessID: int32(info.ProcessID),
		Rect: WindowRect{
			Left:   info.Rect.Left,
			Top:    info.Rect.Top,
			Right:  info.Rect.Right,
			Bottom: info.Rect.Bottom,
		},
	}
}

func sanitizeURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return strings.SplitN(rawURL, "?", 2)[0]
	}
	return parsed.Host
}

func gatewayHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func sessionNameFromID(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	return "sessions/" + sessionID
}

func dataURL(mimeType, image string) string {
	if image == "" {
		return ""
	}
	if mimeType == "" {
		return image
	}
	return "data:" + mimeType + ";base64," + image
}
