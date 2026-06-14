package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"dominion/common/gopkg/otel/tracecontext"
	"dominion/projects/game"
	"dominion/projects/game/desktop/internal/api"
	"dominion/projects/game/desktop/internal/applog"
	"dominion/projects/game/desktop/internal/capture"
	"dominion/projects/game/desktop/internal/operation"
	desktoptrace "dominion/projects/game/desktop/internal/trace"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"google.golang.org/protobuf/encoding/protojson"
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
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Creating session", map[string]any{
		"trace_id":       traceID,
		"correlation_id": corrID,
	})
	session, err := a.client.CreateSession(ctx)
	if err != nil {
		a.logger.Error("backend", "Create session failed", map[string]any{
			"trace_id":       traceID,
			"correlation_id": corrID,
			"error":          err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Session created", map[string]any{
		"session_id":     session.GetSessionId(),
		"trace_id":       traceID,
		"correlation_id": corrID,
	})
	return sessionViewFromProto(session), nil
}

// ListSessions lists sessions with pagination support.
func (a *App) ListSessions(pageSize int, pageToken string) (*ListSessionsView, error) {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Listing sessions", map[string]any{
		"trace_id":       traceID,
		"page_size":      pageSize,
		"page_token":     pageToken,
		"correlation_id": corrID,
	})
	resp, err := a.client.ListSessions(ctx, int32(pageSize), pageToken)
	if err != nil {
		a.logger.Error("backend", "List sessions failed", map[string]any{
			"trace_id":       traceID,
			"correlation_id": corrID,
			"error":          err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Sessions listed", map[string]any{
		"trace_id":       traceID,
		"count":          len(resp.GetSessions()),
		"correlation_id": corrID,
	})
	return listSessionsViewFromProto(resp), nil
}

// GetSession retrieves a session by ID.
func (a *App) GetSession(sessionID string) (*SessionView, error) {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Getting session", map[string]any{
		"trace_id":       traceID,
		"session_id":     sessionID,
		"correlation_id": corrID,
	})
	session, err := a.client.GetSession(ctx, sessionID)
	if err != nil {
		a.logger.Error("backend", "Get session failed", map[string]any{
			"trace_id":       traceID,
			"correlation_id": corrID,
			"error":          err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Session retrieved", map[string]any{
		"session_id":     sessionID,
		"trace_id":       traceID,
		"correlation_id": corrID,
	})
	return sessionViewFromProto(session), nil
}

// DeleteSession deletes a session by ID.
func (a *App) DeleteSession(sessionID string) error {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Deleting session", map[string]any{
		"trace_id":       traceID,
		"session_id":     sessionID,
		"correlation_id": corrID,
	})
	if err := a.client.DeleteSession(ctx, sessionID); err != nil {
		a.logger.Error("backend", "Delete session failed", map[string]any{
			"trace_id":       traceID,
			"correlation_id": corrID,
			"error":          err.Error(),
		})
		return err
	}
	a.logger.Info("backend", "Session deleted", map[string]any{
		"trace_id":       traceID,
		"correlation_id": corrID,
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
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Creating agent", map[string]any{
		"trace_id":       traceID,
		"session_id":     sessionID,
		"correlation_id": corrID,
	})
	agent, err := a.client.CreateAgent(ctx, sessionID)
	if err != nil {
		a.logger.Error("backend", "Create agent failed", map[string]any{
			"trace_id":       traceID,
			"correlation_id": corrID,
			"error":          err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Agent created", map[string]any{
		"session_id":     sessionID,
		"trace_id":       traceID,
		"correlation_id": corrID,
	})
	return agentViewFromProto(agent), nil
}

// CreateAgentWithProfile creates an agent for a session using the specified agent profile.
func (a *App) CreateAgentWithProfile(sessionID string, profileName string) (*AgentView, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if profileName == "" {
		return nil, fmt.Errorf("profile_name is required")
	}
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Creating agent with profile", map[string]any{
		"trace_id":       traceID,
		"session_id":     sessionID,
		"profile_name":   profileName,
		"correlation_id": corrID,
	})
	agent, err := a.client.CreateAgentWithProfile(ctx, sessionID, profileName)
	if err != nil {
		a.logger.Error("backend", "Create agent with profile failed", map[string]any{
			"trace_id":       traceID,
			"correlation_id": corrID,
			"error":          err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Agent created with profile", map[string]any{
		"session_id":     sessionID,
		"profile_name":   profileName,
		"trace_id":       traceID,
		"correlation_id": corrID,
	})
	return agentViewFromProto(agent), nil
}

// ListAgentProfiles lists agent profiles from the prompt service via the gateway REST API.
func (a *App) ListAgentProfiles(pageSize int, pageToken string) (*ListAgentProfilesView, error) {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Listing agent profiles", map[string]any{
		"trace_id":       traceID,
		"page_size":      pageSize,
		"correlation_id": corrID,
	})
	resp, err := a.client.ListAgentProfiles(ctx, int32(pageSize), pageToken)
	if err != nil {
		a.logger.Error("backend", "List agent profiles failed", map[string]any{
			"trace_id":       traceID,
			"correlation_id": corrID,
			"error":          err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Agent profiles listed", map[string]any{
		"trace_id":       traceID,
		"count":          len(resp.GetAgentProfiles()),
		"correlation_id": corrID,
	})
	return listAgentProfilesViewFromProto(resp), nil
}

// CreateAgentProfile creates a new agent profile via the gateway REST API.
func (a *App) CreateAgentProfile(req CreateAgentProfileView) (*AgentProfileView, error) {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Creating agent profile", map[string]any{
		"trace_id":           traceID,
		"correlation_id":     corrID,
		"agent_profile_name": req.AgentProfileName,
	})
	protoReq := &game.CreateAgentProfileRequest{
		AgentProfileName: req.AgentProfileName,
		Model:            req.Model,
		SystemPrompt:     req.SystemPrompt,
		Enabled:          req.Enabled,
	}
	profile, err := a.client.CreateAgentProfile(ctx, protoReq)
	if err != nil {
		a.logger.Error("backend", "Create agent profile failed", map[string]any{
			"trace_id":           traceID,
			"correlation_id":     corrID,
			"agent_profile_name": req.AgentProfileName,
			"error":              err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Agent profile created", map[string]any{
		"trace_id":           traceID,
		"correlation_id":     corrID,
		"agent_profile_name": profile.GetAgentProfileName(),
	})
	return agentProfileViewFromProto(profile), nil
}

// GetAgentProfile retrieves an agent profile by name via the gateway REST API.
func (a *App) GetAgentProfile(agentProfileName string) (*AgentProfileView, error) {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Getting agent profile", map[string]any{
		"trace_id":           traceID,
		"correlation_id":     corrID,
		"agent_profile_name": agentProfileName,
	})
	profile, err := a.client.GetAgentProfile(ctx, agentProfileName)
	if err != nil {
		a.logger.Error("backend", "Get agent profile failed", map[string]any{
			"trace_id":           traceID,
			"correlation_id":     corrID,
			"agent_profile_name": agentProfileName,
			"error":              err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Agent profile retrieved", map[string]any{
		"trace_id":           traceID,
		"correlation_id":     corrID,
		"agent_profile_name": profile.GetAgentProfileName(),
	})
	return agentProfileViewFromProto(profile), nil
}

// DeleteAgentProfile deletes an agent profile by name via the gateway REST API.
func (a *App) DeleteAgentProfile(agentProfileName string) error {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Deleting agent profile", map[string]any{
		"trace_id":           traceID,
		"correlation_id":     corrID,
		"agent_profile_name": agentProfileName,
	})
	err := a.client.DeleteAgentProfile(ctx, agentProfileName)
	if err != nil {
		a.logger.Error("backend", "Delete agent profile failed", map[string]any{
			"trace_id":           traceID,
			"correlation_id":     corrID,
			"agent_profile_name": agentProfileName,
			"error":              err.Error(),
		})
		return err
	}
	a.logger.Info("backend", "Agent profile deleted", map[string]any{
		"trace_id":           traceID,
		"correlation_id":     corrID,
		"agent_profile_name": agentProfileName,
	})
	return nil
}

// SendAgentText sends a text frame to the agent via WebSocket and streams
// back all response frames as Wails "game:frame" events. The loop terminates
// when a wait frame is received (signalling the agent is done) or an error occurs.
func (a *App) SendAgentText(sessionID string, text string) error {
	if a.ws == nil {
		return fmt.Errorf("send agent text: not connected")
	}
	frameID, err := randomHex(8)
	if err != nil {
		return fmt.Errorf("send agent text: %w", err)
	}

	frame := &game.AgentFrame{
		SessionId:  sessionID,
		FrameId:    frameID,
		CreateTime: timestamppb.Now(),
		Sender:     game.FrameSender_FRAME_SENDER_USER,
		Payload: &game.AgentFrame_Text{
			Text: &game.AgentTextFrame{Content: text},
		},
	}

	a.logger.Info("backend", "SendAgentText: sending text frame", map[string]any{
		"session_id": sessionID,
		"frame_id":   frameID,
		"text_len":   len(text),
	})

	if err := a.ws.SendFrame(a.ctx, frame); err != nil {
		a.logger.Error("backend", "SendAgentText: send failed", map[string]any{
			"session_id": sessionID,
			"error":      err.Error(),
		})
		return fmt.Errorf("send agent text: %w", err)
	}

	frameCount := 0
	for {
		resp, err := a.ws.RecvFrame(a.ctx)
		if err != nil {
			a.logger.Error("backend", "SendAgentText: recv error", map[string]any{
				"session_id":  sessionID,
				"frame_count": frameCount,
				"error":       err.Error(),
			})
			runtime.EventsEmit(a.ctx, "game:frame", frameToMap(&game.AgentFrame{
				SessionId: sessionID,
				FrameId:   frameID,
				Payload: &game.AgentFrame_Wait{
					Wait: &game.AgentWaitFrame{},
				},
			}))
			return fmt.Errorf("send agent text: receive: %w", err)
		}
		frameCount++

		runtime.EventsEmit(a.ctx, "game:frame", frameToMap(resp))
		if resp.GetWait() != nil {
			a.logger.Info("backend", "SendAgentText: done", map[string]any{
				"session_id":  sessionID,
				"frame_count": frameCount,
			})
			return nil
		}
	}
}

// GetAgent retrieves the agent for a session.
func (a *App) GetAgent(sessionID string) (*AgentView, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Getting agent", map[string]any{
		"trace_id":       traceID,
		"session_id":     sessionID,
		"correlation_id": corrID,
	})
	agent, err := a.client.GetAgent(ctx, sessionID)
	if err != nil {
		a.logger.Error("backend", "Get agent failed", map[string]any{
			"trace_id":       traceID,
			"correlation_id": corrID,
			"error":          err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Agent retrieved", map[string]any{
		"session_id":     sessionID,
		"trace_id":       traceID,
		"correlation_id": corrID,
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
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Deleting agent", map[string]any{
		"trace_id":       traceID,
		"session_id":     sessionID,
		"correlation_id": corrID,
	})
	if err := a.client.DeleteAgent(ctx, sessionID); err != nil {
		a.logger.Error("backend", "Delete agent failed", map[string]any{
			"trace_id":       traceID,
			"correlation_id": corrID,
			"error":          err.Error(),
		})
		return err
	}
	a.logger.Info("backend", "Agent deleted", map[string]any{
		"trace_id":       traceID,
		"correlation_id": corrID,
	})
	return nil
}

// ListMessages lists all messages for a session's agent.
func (a *App) ListMessages(sessionID string) ([]MessageViewModel, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Listing messages", map[string]any{
		"trace_id":       traceID,
		"session_id":     sessionID,
		"correlation_id": corrID,
	})
	resp, err := a.client.ListMessages(ctx, sessionID)
	if err != nil {
		a.logger.Error("backend", "List messages failed", map[string]any{
			"trace_id":       traceID,
			"correlation_id": corrID,
			"error":          err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Messages listed", map[string]any{
		"session_id":     sessionID,
		"trace_id":       traceID,
		"correlation_id": corrID,
		"count":          len(resp.GetMessages()),
	})
	return ToMessageViewModels(resp.GetMessages()), nil
}

// ListWindows enumerates visible top-level windows (Windows only).
// Returns a not-supported error on other platforms.
func (a *App) ListWindows() ([]capture.WindowRef, error) {
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Listing windows", map[string]any{"correlation_id": corrID})
	windows, err := capture.ListWindows(a.ctx)
	if err != nil {
		a.logger.Error("backend", "List windows failed", map[string]any{"error": err.Error(), "correlation_id": corrID})
		return nil, err
	}
	a.logger.Info("backend", "Windows listed", map[string]any{"count": len(windows), "correlation_id": corrID})
	return windows, nil
}

// BindWindow stores the given window handle as the currently bound window.
// The bound window is used by CaptureScreenshot and SendScreenshot.
func (a *App) BindWindow(hwnd uintptr) error {
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Binding window", map[string]any{"hwnd": hwnd, "correlation_id": corrID})
	// Verify the window still exists by listing and matching.
	windows, err := capture.ListWindows(a.ctx)
	if err != nil {
		a.logger.Error("backend", "Bind window: list windows failed", map[string]any{"error": err.Error(), "correlation_id": corrID})
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
	a.logger.Info("backend", "Window bound", map[string]any{"hwnd": hwnd, "title": a.boundWin.Title, "correlation_id": corrID})
	return nil
}

// CaptureScreenshot captures the currently bound window as a PNG image.
// Returns an error if no window is bound or the capture fails.
func (a *App) CaptureScreenshot() (*capture.CapturedImage, error) {
	if a.boundWin.Handle == 0 {
		return nil, fmt.Errorf("capture screenshot: no window bound")
	}
	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix
	// Capture bounds before screenshot for logging.
	bnds, _ := capture.CaptureWindowBounds(a.boundWin.Handle)
	a.logger.Info("backend", "Capturing screenshot", map[string]any{"hwnd": a.boundWin.Handle, "correlation_id": corrID})
	img, err := capture.CaptureWindow(a.ctx, a.boundWin.Handle)
	if err != nil {
		a.logger.Error("backend", "Capture screenshot failed", map[string]any{"error": err.Error(), "correlation_id": corrID})
		return nil, err
	}
	a.logger.Info("backend", "screenshot captured", map[string]any{
		"hwnd":           a.boundWin.Handle,
		"title":          a.boundWin.Title,
		"bounds":         map[string]int{"left": bnds.Left, "top": bnds.Top, "right": bnds.Right, "bottom": bnds.Bottom},
		"width_px":       img.WidthPx,
		"height_px":      img.HeightPx,
		"encoding":       img.Encoding,
		"size":           len(img.Data),
		"correlation_id": corrID,
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

	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix

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
		"session_id":     a.currentSessionID(),
		"correlation_id": corrID,
		"capture_id":     captureID,
		"frame_id":       frameID,
		"size":           len(img.Data),
	})

	// Send frame via WebSocket.
	if err := a.ws.SendFrame(a.ctx, frame); err != nil {
		a.logger.Error("backend", "Send screenshot frame failed", map[string]any{
			"error":          err.Error(),
			"session_id":     a.currentSessionID(),
			"correlation_id": corrID,
			"frame_id":       frameID,
		})
		return nil, fmt.Errorf("send screenshot: %w", err)
	}

	// Read ack response.
	resp, err := a.ws.RecvFrame(a.ctx)
	if err != nil {
		a.logger.Error("backend", "Receive screenshot ack failed", map[string]any{
			"error":          err.Error(),
			"session_id":     a.currentSessionID(),
			"correlation_id": corrID,
			"frame_id":       frameID,
		})
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
		"capture_id":     captureID,
		"ack_frame_id":   ack.GetAckFrameId(),
		"session_id":     a.currentSessionID(),
		"correlation_id": corrID,
	})
	return ack, nil
}

// ConnectAgent establishes a WebSocket connection for the agent.
// After the WebSocket handshake, it performs an application-level probe
// (round-trip ping) to verify the full path: desktop → gateway → proxy → agent.
// The probe has a 10-second timeout. On failure, the WebSocket is closed
// and no state is stored.
func (a *App) ConnectAgent(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)

	corrID, err := randomHex(8)
	if err != nil {
		corrID = "unknown"
	} else {
		corrID = "corr-" + corrID
	}

	a.logger.Info("backend", "Connecting agent via WebSocket", map[string]any{
		"trace_id":       traceID,
		"session_id":     sessionID,
		"correlation_id": corrID,
	})

	// Close any existing WS connection first.
	if a.ws != nil {
		a.ws.Close()
	}

	ws := &api.WSClient{}
	if err := ws.Connect(ctx, a.cfg.GatewayURL, sessionID, a.cfg.Env); err != nil {
		a.logger.Error("backend", "Connect agent failed", map[string]any{
			"trace_id":       traceID,
			"session_id":     sessionID,
			"correlation_id": corrID,
			"error":          err.Error(),
		})
		return err
	}

	// Application-level probe: send a ping frame and wait for any response.
	// This verifies the full path: desktop → gateway → proxy → agent.
	probeFrameID := "connect-probe-" + corrID[len("corr-"):]
	probeFrame := &game.AgentFrame{
		SessionId:  sessionID,
		FrameId:    probeFrameID,
		CreateTime: timestamppb.Now(),
		Payload: &game.AgentFrame_Status{
			Status: &game.AgentStatusFrame{Status: "ping"},
		},
	}

	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	a.logger.Info("backend", "Sending connect probe", map[string]any{
		"trace_id":       traceID,
		"session_id":     sessionID,
		"frame_id":       probeFrameID,
		"correlation_id": corrID,
	})

	if err := ws.SendFrame(probeCtx, probeFrame); err != nil {
		a.logger.Error("backend", "Connect probe: send failed", map[string]any{
			"trace_id":       traceID,
			"session_id":     sessionID,
			"frame_id":       probeFrameID,
			"correlation_id": corrID,
			"error":          err.Error(),
		})
		ws.Close()
		return fmt.Errorf("connect agent: probe send failed: %w", err)
	}

	resp, err := ws.RecvFrame(probeCtx)
	if err != nil {
		a.logger.Error("backend", "Connect probe: receive failed", map[string]any{
			"trace_id":       traceID,
			"session_id":     sessionID,
			"frame_id":       probeFrameID,
			"correlation_id": corrID,
			"error":          err.Error(),
		})
		ws.Close()
		return fmt.Errorf("connect agent: probe receive failed: %w", err)
	}

	// Accept any response frame — the round-trip itself proves the path is alive.
	a.logger.Info("backend", "Connect probe succeeded", map[string]any{
		"trace_id":          traceID,
		"session_id":        sessionID,
		"frame_id":          probeFrameID,
		"response_frame_id": resp.GetFrameId(),
		"correlation_id":    corrID,
	})

	a.ws = ws
	a.sessionID = sessionID
	a.logger.Info("backend", "Agent connected via WebSocket", map[string]any{
		"trace_id":       traceID,
		"session_id":     sessionID,
		"correlation_id": corrID,
	})
	return nil
}

// SendAgentFrame sends a frame over the WebSocket and returns the response.
func (a *App) SendAgentFrame(frame *game.AgentFrame) (*game.AgentFrame, error) {
	if a.ws == nil {
		return nil, fmt.Errorf("send frame: not connected")
	}
	a.logger.Info("backend", "Sending frame", map[string]any{"session_id": frame.GetSessionId()})
	if err := a.ws.SendFrame(a.ctx, frame); err != nil {
		a.logger.Error("backend", "Send frame failed", map[string]any{"error": err.Error()})
		return nil, err
	}
	resp, err := a.ws.RecvFrame(a.ctx)
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

// Operation result status values for the frontend UI.
const (
	operationResultExecuted int32 = 2
	operationResultFailed   int32 = 4
)

// ExecuteOperation executes a desktop operation (mouse click or key press)
// at the given screenshot-relative coordinates, captures and sends the next
// screenshot via WebSocket to continue the agent loop, and returns the
// result view for the frontend UI.
func (a *App) ExecuteOperation(operationID string, screenshotID string, sequence int64,
	button int32, clickType int32, xPx int32, yPx int32, isMouse bool, keyCodes string,
	windowLeft int32, windowTop int32) *OperationResultView {

	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix

	if isMouse {
		screenX, screenY, err := operation.ScreenshotToScreenCoords(xPx, yPx, windowLeft, windowTop)
		if err != nil {
			a.logger.Error("backend", "ExecuteOperation: coordinate conversion failed", map[string]any{
				"error": err.Error(), "operation_id": operationID, "correlation_id": corrID,
			})
			a.sendNextScreenshot(operationID, corrID)
			return &OperationResultView{
				OperationID: operationID,
				Sequence:    sequence,
				Status:      operationResultFailed,
				Message:     err.Error(),
			}
		}
		if err := operation.ExecuteMouseClick(screenX, screenY, button, clickType); err != nil {
			a.logger.Error("backend", "ExecuteOperation: mouse click failed", map[string]any{
				"error": err.Error(), "operation_id": operationID, "correlation_id": corrID,
			})
			a.sendNextScreenshot(operationID, corrID)
			return &OperationResultView{
				OperationID: operationID,
				Sequence:    sequence,
				Status:      operationResultFailed,
				Message:     err.Error(),
			}
		}
	} else {
		if err := operation.ExecuteKeyPress(keyCodes); err != nil {
			a.logger.Error("backend", "ExecuteOperation: key press failed", map[string]any{
				"error": err.Error(), "operation_id": operationID, "correlation_id": corrID,
			})
			a.sendNextScreenshot(operationID, corrID)
			return &OperationResultView{
				OperationID: operationID,
				Sequence:    sequence,
				Status:      operationResultFailed,
				Message:     err.Error(),
			}
		}
	}

	a.logger.Info("backend", "Operation executed", map[string]any{
		"operation_id":   operationID,
		"screenshot_id":  screenshotID,
		"sequence":       sequence,
		"is_mouse":       isMouse,
		"correlation_id": corrID,
	})

	// Capture and send next screenshot via WebSocket to continue the agent loop.
	a.sendNextScreenshot(operationID, corrID)

	return &OperationResultView{
		OperationID: operationID,
		Sequence:    sequence,
		Status:      operationResultExecuted,
		Message:     "ok",
	}
}

// sendNextScreenshot captures the bound window and sends a new screenshot
// to the agent via WebSocket. Errors are logged but not propagated — the
// agent loop continues on the next tick regardless.
func (a *App) sendNextScreenshot(operationID string, corrID string) {
	if err := a.SendNextScreenshot(); err != nil {
		a.logger.Error("backend", "Send next screenshot failed", map[string]any{
			"error": err.Error(), "operation_id": operationID, "correlation_id": corrID,
		})
	}
}

// SendNextScreenshot captures the bound window and sends a new screenshot
// to the agent via WebSocket. This completes the feedback loop by providing
// the agent with an updated view after an operation has been executed.
func (a *App) SendNextScreenshot() error {
	if a.ws == nil {
		return fmt.Errorf("send next screenshot: not connected")
	}
	if a.boundWin.Handle == 0 {
		return fmt.Errorf("send next screenshot: no window bound")
	}

	corrSuffix, _ := randomHex(8)
	corrID := "corr-" + corrSuffix

	img, err := capture.CaptureWindow(a.ctx, a.boundWin.Handle)
	if err != nil {
		return fmt.Errorf("send next screenshot: %w", err)
	}

	captureID, err := randomHex(8)
	if err != nil {
		return fmt.Errorf("send next screenshot: %w", err)
	}
	frameID, err := randomHex(8)
	if err != nil {
		return fmt.Errorf("send next screenshot: %w", err)
	}

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

	a.logger.Info("backend", "Sending next screenshot", map[string]any{
		"session_id":     a.currentSessionID(),
		"correlation_id": corrID,
		"capture_id":     captureID,
		"frame_id":       frameID,
		"size":           len(img.Data),
	})

	if err := a.ws.SendFrame(a.ctx, frame); err != nil {
		a.logger.Error("backend", "Send next screenshot failed", map[string]any{
			"error":          err.Error(),
			"session_id":     a.currentSessionID(),
			"correlation_id": corrID,
			"frame_id":       frameID,
		})
		return fmt.Errorf("send next screenshot: %w", err)
	}

	a.logger.Info("backend", "Next screenshot sent", map[string]any{
		"capture_id":     captureID,
		"frame_id":       frameID,
		"session_id":     a.currentSessionID(),
		"correlation_id": corrID,
	})
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

// frameToMap serializes a proto AgentFrame to a map[string]any using protojson,
// so that Wails EventsEmit (which uses encoding/json) produces the correct
// camelCase field names and flattens oneof payload fields (e.g. "text", "wait")
// to the top level — matching what the frontend expects.
func frameToMap(frame *game.AgentFrame) map[string]any {
	jsonBytes, err := protojson.Marshal(frame)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var m map[string]any
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return m
}
