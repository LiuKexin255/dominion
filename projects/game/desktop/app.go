package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"dominion/common/gopkg/otel/tracecontext"
	"dominion/projects/game"
	"dominion/projects/game/desktop/internal/api"
	"dominion/projects/game/desktop/internal/applog"
	"dominion/projects/game/desktop/internal/capture"
	desktoptrace "dominion/projects/game/desktop/internal/trace"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// App is the Wails application struct holding all state.
type App struct {
	logger    *applog.Logger
	client    *api.Client
	ws        *api.WSClient
	cfg       api.Config
	ctx       context.Context
	boundWin  capture.WindowRef
	sessionID string // active session from ConnectAgent
}

// NewApp creates a new App with default configuration.
func NewApp(logger *applog.Logger) *App {
	return &App{
		logger: logger,
		cfg: api.Config{
			GatewayURL: "https://game.liukexin.com",
		},
	}
}

// SetContext is called by the Wails OnStartup hook to store the app context.
func (a *App) SetContext(ctx context.Context) {
	a.ctx = ctx
}

// ensureClient lazily creates the API client if not yet initialized.
func (a *App) ensureClient() {
	if a.client != nil {
		return
	}
	a.client = api.NewClient(a.cfg)
}

// GetConfig returns the current configuration.
func (a *App) GetConfig() api.Config {
	a.logger.Info("backend", "GetConfig called")
	return a.cfg
}

// SetConfig updates the configuration and recreates the HTTP client.
func (a *App) SetConfig(cfg api.Config) error {
	if cfg.GatewayURL == "" {
		return fmt.Errorf("set config: GatewayURL is required")
	}
	a.cfg = cfg
	a.client = api.NewClient(cfg)
	a.logger.Info("backend", "Config updated", map[string]any{"gateway_url": cfg.GatewayURL})
	return nil
}

// CreateSession creates a game session via the gateway.
// The session ID is generated server-side.
func (a *App) CreateSession() (*SessionView, error) {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	a.logger.Info("backend", "Creating session", map[string]any{
		"trace_id": traceID,
	})
	session, err := a.client.CreateSession(ctx)
	if err != nil {
		a.logger.Error("backend", "Create session failed", map[string]any{
			"trace_id": traceID,
			"error":    err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Session created", map[string]any{
		"session_id": session.GetSessionId(),
		"trace_id":   traceID,
	})
	return sessionViewFromProto(session), nil
}

// ListSessions lists sessions with pagination support.
func (a *App) ListSessions(pageSize int, pageToken string) (*ListSessionsView, error) {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	a.logger.Info("backend", "Listing sessions", map[string]any{
		"trace_id":   traceID,
		"page_size":  pageSize,
		"page_token": pageToken,
	})
	resp, err := a.client.ListSessions(ctx, int32(pageSize), pageToken)
	if err != nil {
		a.logger.Error("backend", "List sessions failed", map[string]any{
			"trace_id": traceID,
			"error":    err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Sessions listed", map[string]any{
		"trace_id": traceID,
		"count":    len(resp.GetSessions()),
	})
	return listSessionsViewFromProto(resp), nil
}

// GetSession retrieves a session by ID.
func (a *App) GetSession(sessionID string) (*SessionView, error) {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	a.logger.Info("backend", "Getting session", map[string]any{
		"trace_id":   traceID,
		"session_id": sessionID,
	})
	session, err := a.client.GetSession(ctx, sessionID)
	if err != nil {
		a.logger.Error("backend", "Get session failed", map[string]any{
			"trace_id": traceID,
			"error":    err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Session retrieved", map[string]any{
		"session_id": sessionID,
		"trace_id":   traceID,
	})
	return sessionViewFromProto(session), nil
}

// DeleteSession deletes a session by ID.
func (a *App) DeleteSession(sessionID string) error {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	a.logger.Info("backend", "Deleting session", map[string]any{
		"trace_id":   traceID,
		"session_id": sessionID,
	})
	if err := a.client.DeleteSession(ctx, sessionID); err != nil {
		a.logger.Error("backend", "Delete session failed", map[string]any{
			"trace_id": traceID,
			"error":    err.Error(),
		})
		return err
	}
	a.logger.Info("backend", "Session deleted", map[string]any{
		"trace_id": traceID,
	})
	return nil
}

// CreateAgent creates an agent for a session.
func (a *App) CreateAgent(sessionID string) (*AgentView, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	a.logger.Info("backend", "Creating agent", map[string]any{
		"trace_id":   traceID,
		"session_id": sessionID,
	})
	agent, err := a.client.CreateAgent(ctx, sessionID)
	if err != nil {
		a.logger.Error("backend", "Create agent failed", map[string]any{
			"trace_id": traceID,
			"error":    err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Agent created", map[string]any{
		"session_id": sessionID,
		"trace_id":   traceID,
	})
	return agentViewFromProto(agent), nil
}

// GetAgent retrieves the agent for a session.
func (a *App) GetAgent(sessionID string) (*AgentView, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	a.logger.Info("backend", "Getting agent", map[string]any{
		"trace_id":   traceID,
		"session_id": sessionID,
	})
	agent, err := a.client.GetAgent(ctx, sessionID)
	if err != nil {
		a.logger.Error("backend", "Get agent failed", map[string]any{
			"trace_id": traceID,
			"error":    err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Agent retrieved", map[string]any{
		"session_id": sessionID,
		"trace_id":   traceID,
	})
	return agentViewFromProto(agent), nil
}

// DeleteAgent deletes the agent for a session.
func (a *App) DeleteAgent(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	a.logger.Info("backend", "Deleting agent", map[string]any{
		"trace_id":   traceID,
		"session_id": sessionID,
	})
	if err := a.client.DeleteAgent(ctx, sessionID); err != nil {
		a.logger.Error("backend", "Delete agent failed", map[string]any{
			"trace_id": traceID,
			"error":    err.Error(),
		})
		return err
	}
	a.logger.Info("backend", "Agent deleted", map[string]any{
		"trace_id": traceID,
	})
	return nil
}

// ListWindows enumerates visible top-level windows (Windows only).
// Returns a not-supported error on other platforms.
func (a *App) ListWindows() ([]capture.WindowRef, error) {
	a.logger.Info("backend", "Listing windows")
	windows, err := capture.ListWindows(a.ctx)
	if err != nil {
		a.logger.Error("backend", "List windows failed", map[string]any{"error": err.Error()})
		return nil, err
	}
	a.logger.Info("backend", "Windows listed", map[string]any{"count": len(windows)})
	return windows, nil
}

// BindWindow stores the given window handle as the currently bound window.
// The bound window is used by CaptureScreenshot and SendScreenshot.
func (a *App) BindWindow(hwnd uintptr) error {
	a.logger.Info("backend", "Binding window", map[string]any{"hwnd": hwnd})
	// Verify the window still exists by listing and matching.
	windows, err := capture.ListWindows(a.ctx)
	if err != nil {
		a.logger.Error("backend", "Bind window: list windows failed", map[string]any{"error": err.Error()})
		return fmt.Errorf("bind window: %w", err)
	}
	found := false
	for _, w := range windows {
		if w.Handle == hwnd {
			a.boundWin = w
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("bind window: hwnd %d not found in visible windows", hwnd)
	}
	a.logger.Info("backend", "Window bound", map[string]any{"hwnd": hwnd, "title": a.boundWin.Title})
	return nil
}

// CaptureScreenshot captures the currently bound window as a PNG image.
// Returns an error if no window is bound or the capture fails.
func (a *App) CaptureScreenshot() (*capture.CapturedImage, error) {
	if a.boundWin.Handle == 0 {
		return nil, fmt.Errorf("capture screenshot: no window bound")
	}
	// Capture bounds before screenshot for logging.
	bnds, _ := capture.CaptureWindowBounds(a.boundWin.Handle)
	a.logger.Info("backend", "Capturing screenshot", map[string]any{"hwnd": a.boundWin.Handle})
	img, err := capture.CaptureWindow(a.ctx, a.boundWin.Handle)
	if err != nil {
		a.logger.Error("backend", "Capture screenshot failed", map[string]any{"error": err.Error()})
		return nil, err
	}
	a.logger.Info("backend", "screenshot captured", map[string]any{
		"hwnd":      a.boundWin.Handle,
		"title":     a.boundWin.Title,
		"bounds":    map[string]int{"left": bnds.Left, "top": bnds.Top, "right": bnds.Right, "bottom": bnds.Bottom},
		"width_px":  img.WidthPx,
		"height_px": img.HeightPx,
		"encoding":  img.Encoding,
		"size":      len(img.Data),
	})
	return img, nil
}

// SendScreenshot captures the bound window, encodes as PNG, and sends it
// to the agent via WebSocket. It waits for an ack frame with a matching
// capture_id and returns the ack.
func (a *App) SendScreenshot(hwnd uintptr) (*game.AgentAckFrame, error) {
	if a.ws == nil {
		return nil, fmt.Errorf("send screenshot: not connected")
	}

	// Capture the window.
	img, err := capture.CaptureWindow(a.ctx, hwnd)
	if err != nil {
		return nil, fmt.Errorf("send screenshot: %w", err)
	}

	// Generate capture_id and frame_id via crypto/rand (8 bytes → 16 hex chars).
	captureID, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("send screenshot: %w", err)
	}
	frameID, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("send screenshot: %w", err)
	}

	// Build the AgentFrame with a screenshot payload.
	frame := &game.AgentFrame{
		SessionId:  a.currentSessionID(),
		FrameId:    frameID,
		CreateTime: timestamppb.Now(),
		Payload: &game.AgentFrame_Screenshot{
			Screenshot: &game.AgentScreenshotFrame{
				CaptureId:   captureID,
				Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:        img.Data,
				WidthPx:     int32(img.WidthPx),
				HeightPx:    int32(img.HeightPx),
				ScaleFactor: a.boundWin.ScaleFactor,
				WindowTitle: a.boundWin.Title,
				CaptureTime: timestamppb.Now(),
			},
		},
	}

	a.logger.Info("backend", "Sending screenshot", map[string]any{
		"capture_id": captureID,
		"frame_id":   frameID,
		"size":       len(img.Data),
	})

	// Send frame via WebSocket.
	if err := a.ws.SendFrame(frame); err != nil {
		a.logger.Error("backend", "Send screenshot frame failed", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("send screenshot: %w", err)
	}

	// Read ack response.
	resp, err := a.ws.RecvFrame()
	if err != nil {
		a.logger.Error("backend", "Receive screenshot ack failed", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("send screenshot: %w", err)
	}

	// Verify ack contains matching capture_id.
	ack := resp.GetAck()
	if ack == nil {
		return nil, fmt.Errorf("send screenshot: expected ack frame, got %T", resp.GetPayload())
	}
	if ack.GetAckFrameId() != captureID {
		return nil, fmt.Errorf("send screenshot: ack frame_id mismatch: expected %s, got %s", captureID, ack.GetAckFrameId())
	}

	a.logger.Info("backend", "Screenshot ack received", map[string]any{
		"capture_id":   captureID,
		"ack_frame_id": ack.GetAckFrameId(),
	})
	return ack, nil
}

// GetAgentStatus retrieves the agent status for a session.
func (a *App) GetAgentStatus(sessionID string) (*game.AgentStatus, error) {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	a.logger.Info("backend", "Getting agent status", map[string]any{
		"trace_id":   traceID,
		"session_id": sessionID,
	})
	status, err := a.client.GetAgentStatus(ctx, sessionID)
	if err != nil {
		a.logger.Error("backend", "Get agent status failed", map[string]any{
			"trace_id": traceID,
			"error":    err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Agent status retrieved", map[string]any{
		"trace_id":   traceID,
		"session_id": sessionID,
	})
	return status, nil
}

// ConnectAgent establishes a WebSocket connection for the agent.
// It stores the sessionID for subsequent SendScreenshot calls.
func (a *App) ConnectAgent(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	a.logger.Info("backend", "Connecting agent via WebSocket", map[string]any{
		"trace_id":   traceID,
		"session_id": sessionID,
	})

	// Close any existing WS connection first.
	if a.ws != nil {
		a.ws.Close()
	}

	ws := &api.WSClient{}
	if err := ws.Connect(ctx, a.cfg.GatewayURL, sessionID, a.cfg.Env); err != nil {
		a.logger.Error("backend", "Connect agent failed", map[string]any{
			"trace_id": traceID,
			"error":    err.Error(),
		})
		return err
	}
	a.ws = ws
	a.sessionID = sessionID
	a.logger.Info("backend", "Agent connected via WebSocket", map[string]any{
		"trace_id": traceID,
	})
	return nil
}

// SendAgentFrame sends a frame over the WebSocket and returns the response.
func (a *App) SendAgentFrame(frame *game.AgentFrame) (*game.AgentFrame, error) {
	if a.ws == nil {
		return nil, fmt.Errorf("send frame: not connected")
	}
	a.logger.Info("backend", "Sending frame", map[string]any{"session_id": frame.GetSessionId()})
	if err := a.ws.SendFrame(frame); err != nil {
		a.logger.Error("backend", "Send frame failed", map[string]any{"error": err.Error()})
		return nil, err
	}
	resp, err := a.ws.RecvFrame()
	if err != nil {
		a.logger.Error("backend", "Receive frame failed", map[string]any{"error": err.Error()})
		return nil, err
	}
	a.logger.Info("backend", "Frame received", map[string]any{"session_id": resp.GetSessionId()})
	return resp, nil
}

// CloseAgent closes the WebSocket connection.
func (a *App) CloseAgent() error {
	if a.ws == nil {
		return nil
	}
	a.logger.Info("backend", "Closing agent WebSocket")
	if err := a.ws.Close(); err != nil {
		a.logger.Error("backend", "Close agent failed", map[string]any{"error": err.Error()})
		return err
	}
	a.ws = nil
	return nil
}

// Logs returns all current log entries.
func (a *App) Logs() []applog.Entry {
	return a.logger.Entries()
}

// currentSessionID returns the session ID associated with the current WS connection.
func (a *App) currentSessionID() string {
	return a.sessionID
}

// randomHex generates n random bytes and returns them as a hex string (2*n chars).
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random hex: %w", err)
	}
	return hex.EncodeToString(b), nil
}
