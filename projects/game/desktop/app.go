package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"dominion/common/gopkg/otel/tracecontext"
	"dominion/projects/game"
	"dominion/projects/game/desktop/internal/api"
	"dominion/projects/game/desktop/internal/applog"
	"dominion/projects/game/desktop/internal/capture"
	"dominion/projects/game/desktop/internal/operation"
	desktoptrace "dominion/projects/game/desktop/internal/trace"
	gameconst "dominion/projects/game/pkg/gameconst"

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
	sessionID string // active session set on WebSocket connect
	recvDone  chan struct{}
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
	corrSuffix, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
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
	corrSuffix, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
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
	corrSuffix, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
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
	corrSuffix, err := randomHex(8)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
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

// ListAgentProfiles lists agent profiles from the prompt service via the gateway REST API.
func (a *App) ListAgentProfiles(pageSize int, pageToken string) (*ListAgentProfilesView, error) {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("list agent profiles: %w", err)
	}
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
	corrSuffix, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("create agent profile: %w", err)
	}
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
		ToolNames:        req.ToolNames,
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
	createdID, _ := gameconst.AgentProfileID(profile.GetName())
	a.logger.Info("backend", "Agent profile created", map[string]any{
		"trace_id":           traceID,
		"correlation_id":     corrID,
		"agent_profile_name": createdID,
	})
	return agentProfileViewFromProto(profile), nil
}

// GetAgentProfile retrieves an agent profile by name via the gateway REST API.
func (a *App) GetAgentProfile(agentProfileName string) (*AgentProfileView, error) {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("get agent profile: %w", err)
	}
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
	gotID, _ := gameconst.AgentProfileID(profile.GetName())
	a.logger.Info("backend", "Agent profile retrieved", map[string]any{
		"trace_id":           traceID,
		"correlation_id":     corrID,
		"agent_profile_name": gotID,
	})
	return agentProfileViewFromProto(profile), nil
}

// DeleteAgentProfile deletes an agent profile by name via the gateway REST API.
func (a *App) DeleteAgentProfile(agentProfileName string) error {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, err := randomHex(8)
	if err != nil {
		return fmt.Errorf("delete agent profile: %w", err)
	}
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Deleting agent profile", map[string]any{
		"trace_id":           traceID,
		"correlation_id":     corrID,
		"agent_profile_name": agentProfileName,
	})
	err = a.client.DeleteAgentProfile(ctx, agentProfileName)
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

// UpdateAgentProfile partially updates an agent profile via PATCH.
// Per grpc-gateway binding the profile fields are sent as the PATCH body and
// updateMaskPaths are sent as repeated update_mask.paths query parameters.
func (a *App) UpdateAgentProfile(agentProfileName string, profile AgentProfileView, updateMaskPaths []string) (*AgentProfileView, error) {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("update agent profile: %w", err)
	}
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Updating agent profile", map[string]any{
		"trace_id":           traceID,
		"correlation_id":     corrID,
		"agent_profile_name": agentProfileName,
		"update_mask":        updateMaskPaths,
	})
	protoProfile := &game.AgentProfile{
		Model:        profile.Model,
		SystemPrompt: profile.SystemPrompt,
		SkillNames:   profile.SkillNames,
		McpNames:     profile.McpNames,
		ToolNames:    profile.ToolNames,
		Enabled:      profile.Enabled,
	}
	updated, err := a.client.UpdateAgentProfile(ctx, agentProfileName, protoProfile, updateMaskPaths)
	if err != nil {
		a.logger.Error("backend", "Update agent profile failed", map[string]any{
			"trace_id":           traceID,
			"correlation_id":     corrID,
			"agent_profile_name": agentProfileName,
			"error":              err.Error(),
		})
		return nil, err
	}
	a.logger.Info("backend", "Agent profile updated", map[string]any{
		"trace_id":           traceID,
		"correlation_id":     corrID,
		"agent_profile_name": agentProfileName,
	})
	return agentProfileViewFromProto(updated), nil
}

// RefreshAgent refreshes the agent for a session so it reloads its adapter with
// the latest profile configuration. Called by the UI after a profile update.
func (a *App) RefreshAgent(sessionID string) error {
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, err := randomHex(8)
	if err != nil {
		return fmt.Errorf("refresh agent: %w", err)
	}
	corrID := "corr-" + corrSuffix
	a.logger.Info("backend", "Refreshing agent", map[string]any{
		"trace_id":       traceID,
		"correlation_id": corrID,
		"session_id":     sessionID,
	})
	if err := a.client.RefreshAgent(ctx, sessionID); err != nil {
		a.logger.Error("backend", "Refresh agent failed", map[string]any{
			"trace_id":       traceID,
			"correlation_id": corrID,
			"session_id":     sessionID,
			"error":          err.Error(),
		})
		return err
	}
	a.logger.Info("backend", "Agent refreshed", map[string]any{
		"trace_id":       traceID,
		"correlation_id": corrID,
		"session_id":     sessionID,
	})
	return nil
}

// maxScreenshotBytes is the maximum allowed screenshot payload for a single
// user turn (5 MiB). Per FR-005a the desktop rejects oversized screenshots
// before any WebSocket send.
const maxScreenshotBytes = 5 * 1024 * 1024

// SendUserTurn sends a single user turn bundling text and an optional
// screenshot to the agent via WebSocket, then returns immediately. The
// inbound response frames are drained asynchronously by recvLoop, which
// emits each as a Wails "game:frame" event and terminates when a wait
// frame is received (signalling the agent is done) or RecvFrame errors.
//
// The agentProfileName selects which agent profile to use for this session.
// screenshotData is the raw PNG bytes of the bound window; pass an empty
// slice when no screenshot is attached. screenshotWidth and screenshotHeight
// describe the pixel dimensions of screenshotData and are ignored when it is
// empty.
//
// Inbound AgentOperationFrame payloads are auto-executed by recvLoop and a
// matching AgentOperationResultFrame is sent back over the same WebSocket
// connection (FR-013). The result frame carries a post-action screenshot of
// the bound window (FR-007).
func (a *App) SendUserTurn(sessionID string, text string, screenshotData []byte, screenshotWidth int, screenshotHeight int, agentProfileName string) error {
	if a.ws == nil {
		return fmt.Errorf("send user turn: not connected")
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("send user turn: text is required and cannot be empty")
	}
	if len(screenshotData) > maxScreenshotBytes {
		return fmt.Errorf("send user turn: screenshot size %d exceeds 5 MiB limit (%d)", len(screenshotData), maxScreenshotBytes)
	}

	frameID, err := randomHex(8)
	if err != nil {
		return fmt.Errorf("send user turn: %w", err)
	}

	userTurn := &game.AgentUserTurnFrame{
		Text: text,
	}
	if len(screenshotData) > 0 {
		userTurn.Image = &game.AgentImageFrame{
			Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
			Data:        screenshotData,
			WidthPx:     int32(screenshotWidth),
			HeightPx:    int32(screenshotHeight),
			ScaleFactor: a.boundWin.ScaleFactor,
			WindowTitle: a.boundWin.Title,
		}
	}

	frame := &game.AgentFrame{
		SessionId:        sessionID,
		FrameId:          frameID,
		CreateTime:       timestamppb.Now(),
		Sender:           game.FrameSender_FRAME_SENDER_USER,
		AgentProfileName: agentProfileName,
		Payload: &game.AgentFrame_UserTurn{
			UserTurn: userTurn,
		},
	}

	a.logger.Info("backend", "SendUserTurn: sending user turn frame", map[string]any{
		"session_id":       sessionID,
		"frame_id":         frameID,
		"text_len":         len(text),
		"screenshot_bytes": len(screenshotData),
	})

	if err := a.ws.SendFrame(a.ctx, frame); err != nil {
		a.logger.Error("backend", "SendUserTurn: send failed", map[string]any{
			"session_id": sessionID,
			"error":      err.Error(),
		})
		return fmt.Errorf("send user turn: %w", err)
	}

	// Drain inbound frames asynchronously so this call returns immediately.
	// recvLoop closes recvDone when it exits (wait frame received or error);
	// CloseAgent waits on recvDone after tearing the socket down so the
	// blocked RecvFrame unblocks instead of deadlocking.
	a.recvDone = make(chan struct{})
	go a.recvLoop(sessionID, frameID)
	return nil
}

// recvLoop drains inbound WebSocket frames for an in-flight user turn and
// emits each as a "game:frame" event. It runs in its own goroutine launched
// by SendUserTurn. The loop terminates — and closes recvDone — when a wait
// frame is received (the agent is done) or RecvFrame errors.
//
// On RecvFrame error a synthesized wait frame is emitted so the frontend can
// settle the turn before the failure surfaces, preserving the behavior the
// synchronous loop previously had.
func (a *App) recvLoop(sessionID, frameID string) {
	defer close(a.recvDone)

	frameCount := 0
	for {
		resp, err := a.ws.RecvFrame(a.ctx)
		if err != nil {
			a.logger.Error("backend", "recvLoop: recv error", map[string]any{
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
			return
		}
		frameCount++

		runtime.EventsEmit(a.ctx, "game:frame", frameToMap(resp))

		// Inbound operation: auto-execute and report the result with a
		// post-action screenshot (FR-007, FR-013).
		if op := resp.GetOperation(); op != nil {
			if err := a.handleInboundOperation(sessionID, op); err != nil {
				a.logger.Error("backend", "recvLoop: handle inbound operation failed", map[string]any{
					"session_id":  sessionID,
					"frame_count": frameCount,
					"error":       err.Error(),
				})
				return
			}
			continue
		}

		if resp.GetWait() != nil {
			a.logger.Info("backend", "recvLoop: done", map[string]any{
				"session_id":  sessionID,
				"frame_count": frameCount,
			})
			return
		}
	}
}

// handleInboundOperation executes an inbound AgentOperationFrame using the
// existing operation executor and sends the matching AgentOperationResultFrame
// back over the WebSocket. The result frame carries the same operation_id and
// a SUCCEEDED/FAILED status; it is never carried by an AgentAckFrame (FR-013).
// A post-action screenshot is attached when the bound window can be
// captured (FR-007).
func (a *App) handleInboundOperation(sessionID string, op *game.AgentOperationFrame) error {
	result := a.executeAgentOperation(op)

	resultFrameID, err := randomHex(8)
	if err != nil {
		a.logger.Error("backend", "handleInboundOperation: frame id failed", map[string]any{
			"session_id":   sessionID,
			"operation_id": result.GetOperationId(),
			"error":        err.Error(),
		})
		return fmt.Errorf("send user turn: operation result: %w", err)
	}

	resultFrame := &game.AgentFrame{
		SessionId:  sessionID,
		FrameId:    resultFrameID,
		CreateTime: timestamppb.Now(),
		Sender:     game.FrameSender_FRAME_SENDER_USER,
		Payload: &game.AgentFrame_OperationResult{
			OperationResult: result,
		},
	}

	if err := a.ws.SendFrame(a.ctx, resultFrame); err != nil {
		a.logger.Error("backend", "handleInboundOperation: send failed", map[string]any{
			"session_id":   sessionID,
			"operation_id": result.GetOperationId(),
			"error":        err.Error(),
		})
		return fmt.Errorf("send user turn: operation result: %w", err)
	}
	return nil
}

// executeAgentOperation runs an inbound mouse operation via the split
// move/click executor and returns the matching result frame. A MOVE action
// captures the bound window's bounds, converts the screenshot-relative target
// to screen-absolute coordinates, and repositions the cursor; any click
// action dispatches button events at the cursor's current position with no
// coordinate conversion.
//
// After the action phase — regardless of whether it succeeded — a follow-up
// screenshot of the bound window is captured (FR-007). The screenshot is
// attached to the result frame when capture and sizing succeed; otherwise
// the capture failure is recorded in the result message. Status always
// reflects the ACTION outcome (never SUCCEEDED when the action failed).
// Precondition failures (no mouse payload, no window bound) return early
// since no screenshot is possible without a bound window.
func (a *App) executeAgentOperation(op *game.AgentOperationFrame) *game.AgentOperationResultFrame {
	operationID := op.GetOperationId()

	corrSuffix, err := randomHex(8)
	corrID := "corr-unknown"
	if err != nil {
		a.logger.Error("backend", "executeAgentOperation: correlation id failed", map[string]any{"error": err.Error()})
	} else {
		corrID = "corr-" + corrSuffix
	}

	failed := func(msg string) *game.AgentOperationResultFrame {
		a.logger.Error("backend", "executeAgentOperation: failed", map[string]any{
			"operation_id":   operationID,
			"correlation_id": corrID,
			"error":          msg,
		})
		return &game.AgentOperationResultFrame{
			OperationId: operationID,
			Status:      game.AgentOperationResultStatus_AGENT_OPERATION_RESULT_STATUS_FAILED,
			Message:     msg,
		}
	}

	mouse := op.GetMouse()
	if mouse == nil {
		return failed("unsupported operation: only mouse operations are supported")
	}

	if a.boundWin.Handle == 0 {
		return failed("no window bound")
	}

	// Action phase: accumulate errors instead of early-returning so the
	// screenshot phase always runs (FR-007). actionStatus reflects only the
	// ACTION outcome; a failed action never reports SUCCEEDED.
	//
	// MOVE captures the bound window's bounds to translate the
	// screenshot-relative target into screen-absolute coordinates, then moves
	// the cursor. Click actions dispatch button events at the cursor's current
	// position and perform no coordinate conversion or cursor repositioning.
	var actionErr error
	var screenX, screenY int32
	var bounds capture.WindowBounds
	if mouse.GetAction() == game.AgentMouseAction_AGENT_MOUSE_ACTION_MOVE {
		var bErr error
		bounds, bErr = capture.CaptureWindowBounds(a.boundWin.Handle)
		if bErr != nil {
			actionErr = fmt.Errorf("capture window bounds: %w", bErr)
		} else {
			var cErr error
			screenX, screenY, cErr = operation.ScreenshotToScreenCoords(mouse.GetXPx(), mouse.GetYPx(), int32(bounds.Left), int32(bounds.Top))
			if cErr != nil {
				actionErr = fmt.Errorf("coordinate conversion: %w", cErr)
			} else if eErr := operation.MoveCursor(screenX, screenY); eErr != nil {
				actionErr = fmt.Errorf("move cursor: %w", eErr)
			}
		}
	} else {
		// Synthetic clicks (SendInput) are consumed by Windows for window
		// activation when the target is not the foreground window, so the
		// bound window must be foreground before the button event fires —
		// otherwise the click lands as an activation gesture with no
		// application-level effect. The cursor position from the preceding
		// mouse_move is preserved by SetForeground.
		fgBefore := capture.ForegroundWindow()
		fgOk := capture.SetForeground(a.boundWin.Handle)
		fgAfter := capture.ForegroundWindow()
		a.logger.Info("backend", "click: foreground state", map[string]any{
			"operation_id":      operationID,
			"correlation_id":    corrID,
			"window_handle":     a.boundWin.Handle,
			"window_title":      a.boundWin.Title,
			"foreground_before": fgBefore,
			"set_foreground_ok": fgOk,
			"foreground_after":  fgAfter,
		})
		if eErr := operation.ExecuteClickAtCurrentPos(mouse.GetAction()); eErr != nil {
			actionErr = fmt.Errorf("click action: %w", eErr)
		}
	}

	actionStatus := game.AgentOperationResultStatus_AGENT_OPERATION_RESULT_STATUS_SUCCEEDED
	actionMsg := "ok"
	if actionErr != nil {
		actionStatus = game.AgentOperationResultStatus_AGENT_OPERATION_RESULT_STATUS_FAILED
		actionMsg = actionErr.Error()
		a.logger.Error("backend", "executeAgentOperation: action failed", map[string]any{
			"operation_id":   operationID,
			"correlation_id": corrID,
			"error":          actionErr.Error(),
		})
	} else {
		a.logger.Info("backend", "Operation executed", map[string]any{
			"operation_id":  operationID,
			"action":        mouse.GetAction().String(),
			"screenshot_x":  mouse.GetXPx(),
			"screenshot_y":  mouse.GetYPx(),
			"window_handle": a.boundWin.Handle,
			"window_title":  a.boundWin.Title,
			"window_bounds": map[string]int{
				"left":   bounds.Left,
				"top":    bounds.Top,
				"right":  bounds.Right,
				"bottom": bounds.Bottom,
				"width":  bounds.Right - bounds.Left,
				"height": bounds.Bottom - bounds.Top,
			},
			"screen_x":       screenX,
			"screen_y":       screenY,
			"correlation_id": corrID,
		})
	}

	// Single exit: build the result with the accumulated action status, then
	// always attempt a post-action screenshot when a window is bound (FR-007).
	result := &game.AgentOperationResultFrame{
		OperationId: operationID,
		Status:      actionStatus,
		Message:     actionMsg,
	}

	if a.boundWin.Handle != 0 {
		capturedImg, captureErr := capture.CaptureWindow(a.ctx, a.boundWin.Handle)
		switch {
		case captureErr != nil:
			result.Message = fmt.Sprintf("%s (screenshot capture failed: %s)", result.Message, captureErr.Error())
		case len(capturedImg.Data) > maxScreenshotBytes:
			result.Message = fmt.Sprintf("%s (screenshot exceeds 5 MiB limit)", result.Message)
		default:
			result.Screenshot = &game.AgentImageFrame{
				Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:        capturedImg.Data,
				WidthPx:     int32(capturedImg.WidthPx),
				HeightPx:    int32(capturedImg.HeightPx),
				ScaleFactor: a.boundWin.ScaleFactor,
				WindowTitle: a.boundWin.Title,
			}
		}
	}

	return result
}

// GetAgent retrieves the agent for a session.
func (a *App) GetAgent(sessionID string) (*AgentView, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
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

// ListMessages lists all messages for a session's agent.
func (a *App) ListMessages(sessionID string) ([]*MessageViewModel, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	a.ensureClient()
	ctx := tracecontext.Ensure(a.ctx)
	traceID := desktoptrace.TraceIDFromContext(ctx)
	corrSuffix, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
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
	corrSuffix, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("list windows: %w", err)
	}
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
// The bound window is used by CaptureScreenshot and SendUserTurn.
func (a *App) BindWindow(hwnd uintptr) error {
	corrSuffix, err := randomHex(8)
	if err != nil {
		return fmt.Errorf("bind window: %w", err)
	}
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
	corrSuffix, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("capture screenshot: %w", err)
	}
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

// ConnectAgent establishes a WebSocket connection for the session.
// Connects directly without prior agent creation — the agent profile is
// specified on first SendUserTurn instead.
// After the WebSocket handshake, it performs an application-level probe
// (round-trip ping) to verify the full path: desktop → gateway → proxy.
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
		a.logger.Error("backend", "ConnectAgent: failed to generate correlation id", map[string]any{"error": err.Error()})
		corrID = "corr-unknown"
	} else {
		corrID = "corr-" + corrID
	}

	a.logger.Info("backend", "Connecting session via WebSocket", map[string]any{
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
		a.logger.Error("backend", "Connect session failed", map[string]any{
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
		return fmt.Errorf("connect session: probe send failed: %w", err)
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
		return fmt.Errorf("connect session: probe receive failed: %w", err)
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
	a.logger.Info("backend", "Session connected via WebSocket", map[string]any{
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
	// ws.Close() tears the socket down, which unblocks any in-flight
	// RecvFrame in recvLoop; the goroutine then emits a synthesized wait
	// frame and closes recvDone. Waiting here avoids clearing a.ws while
	// recvLoop may still be reading it.
	if a.recvDone != nil {
		<-a.recvDone
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
