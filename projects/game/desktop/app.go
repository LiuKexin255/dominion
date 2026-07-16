package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"dominion/common/gopkg/otel/tracecontext"
	"dominion/projects/game"
	"dominion/projects/game/desktop/internal/api"
	"dominion/projects/game/desktop/internal/applog"
	"dominion/projects/game/desktop/internal/capture"
	"dominion/projects/game/desktop/internal/chatstream"
	"dominion/projects/game/desktop/internal/operation"
	desktoptrace "dominion/projects/game/desktop/internal/trace"
	gameconst "dominion/projects/game/pkg/gameconst"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// App is the Wails application struct holding all state.
type App struct {
	logger      *applog.Logger
	client      *api.Client
	ws          *api.WSClient
	cfg         api.Config
	ctx         context.Context
	boundWin    capture.WindowRef
	sessionID   string // active session set on WebSocket connect
	recvDone    chan struct{}
	chatStreams *chatstream.Registry
	chatServer  *chatstream.Server
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

// SetChatStream injects the chatstream Registry and Server into the App.
// Called once from main.go OnStartup before the frontend binds.
func (a *App) SetChatStream(reg *chatstream.Registry, srv *chatstream.Server) {
	a.chatStreams = reg
	a.chatServer = srv
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
	// PromptsParent is the AIP-156 singleton-namespace parent literal bound
	// by the proto URI template {parent=prompts}; no Prompt resource exists.
	protoReq := &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: req.AgentProfileName,
		AgentProfile: &game.AgentProfile{
			Model:        req.Model,
			SystemPrompt: req.SystemPrompt,
			Enabled:      req.Enabled,
			ToolNames:    req.ToolNames,
		},
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

// postActionScreenshotDelay is the pause between completing a mouse action
// and capturing the post-action screenshot. Waiting briefly lets the target
// application finish rendering the result of the action before the screenshot
// is taken.
const postActionScreenshotDelay = 500 * time.Millisecond

// SendUserTurn sends a single user turn bundling text and an optional
// screenshot to the agent via WebSocket, then returns immediately. The
// inbound response frames are drained asynchronously by recvLoop, which
// emits each as a Wails "game:frame" event and terminates when a wait
// signal is received (signalling the agent is done) or RecvFrame errors.
//
// The agentProfileName selects which agent profile to use for this session.
// screenshotData is the raw PNG bytes of the bound window; pass an empty
// slice when no screenshot is attached. screenshotWidth and screenshotHeight
// describe the pixel dimensions of screenshotData and are ignored when it is
// empty.
//
// The user turn is carried as a content frame whose PartBlock holds a
// TextPart and, when a screenshot is attached, an ImagePart. Inbound
// MouseMovePart/MouseClickPart payloads are auto-executed by recvLoop and a
// matching ToolResultPart is sent back over the same WebSocket connection
// (FR-013). The result part carries a post-action screenshot of the bound
// window (FR-007).
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

	parts := []*game.Part{
		{Kind: &game.Part_Text{Text: &game.TextPart{Content: text}}},
	}
	if len(screenshotData) > 0 {
		parts = append(parts, &game.Part{
			Kind: &game.Part_Image{Image: &game.ImagePart{
				Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:        screenshotData,
				WidthPx:     int32(screenshotWidth),
				HeightPx:    int32(screenshotHeight),
				ScaleFactor: a.boundWin.ScaleFactor,
				WindowTitle: a.boundWin.Title,
			}},
		})
	}

	frame := &game.AgentFrame{
		SessionId:        sessionID,
		FrameId:          frameID,
		CreateTime:       timestamppb.Now(),
		Sender:           game.FrameSender_FRAME_SENDER_USER,
		AgentProfileName: agentProfileName,
		Payload: &game.AgentFrame_Content{
			Content: &game.PartBlock{Parts: parts},
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
	// recvLoop closes recvDone when it exits (wait signal received or error);
	// CloseAgent waits on recvDone after tearing the socket down so the
	// blocked RecvFrame unblocks instead of deadlocking.
	a.recvDone = make(chan struct{})
	go a.recvLoop(sessionID, frameID)
	return nil
}

// recvLoop drains inbound WebSocket frames for an in-flight user turn and
// appends each to the session's chat stream. It runs in its own goroutine
// launched by SendUserTurn. The loop terminates — and closes recvDone —
// when a wait signal is received (the agent is done) or RecvFrame errors.
//
// Frames carry exactly one payload (PartBlock content OR a single control
// signal). Content frames are scanned for tool requests: MouseMovePart and
// MouseClickPart are auto-executed and their ToolResultPart is sent back
// (FR-007, FR-013); the remaining parts (text/thinking/image) are surfaced
// via the appended frame only. Wait/Warn/Status signals are forwarded via
// the appended frame; a wait signal additionally ends the turn.
//
// On RecvFrame error a synthesized wait signal is appended so the frontend
// can settle the turn before the failure surfaces, preserving the behavior
// the synchronous loop previously had.
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
			a.chatStreams.Append(sessionID, &game.AgentFrame{
				SessionId: sessionID,
				FrameId:   frameID,
				Payload: &game.AgentFrame_Wait{
					Wait: &game.WaitSignal{},
				},
			})
			return
		}
		frameCount++

		a.chatStreams.Append(sessionID, resp)

		switch payload := resp.GetPayload().(type) {
		case *game.AgentFrame_Content:
			// Auto-execute tool requests (mousemove/mouseclick/keypress) and
			// report each result with a post-action screenshot (FR-007,
			// FR-013). A PartBlock carries one or more operation parts
			// sharing a single tool_id (data-model.md §5c): a saolei
			// WINDOW_MESSAGE move+click combo is one atomic group; the
			// existing single-part mouse tools are one-element groups.
			groups := groupOperationPartsByToolID(payload.Content.GetParts())
			for _, group := range groups {
				if err := a.handleInboundOperation(sessionID, group); err != nil {
					a.logger.Error("backend", "recvLoop: handle inbound operation failed", map[string]any{
						"session_id":  sessionID,
						"frame_count": frameCount,
						"error":       err.Error(),
					})
					return
				}
			}
		case *game.AgentFrame_Wait:
			a.logger.Info("backend", "recvLoop: done", map[string]any{
				"session_id":  sessionID,
				"frame_count": frameCount,
			})
			return
		case *game.AgentFrame_Warn, *game.AgentFrame_Status:
			// Forwarded via the appended frame above; nothing else to do.
		}
	}
}

// handleInboundOperation executes one atomic tool-request group (one or more
// operation Parts sharing a tool_id) and sends the matching ToolResultPart
// back over the WebSocket wrapped in a content frame. The result part carries
// the group's tool_id and a SUCCEEDED/FAILED status; it is never carried by an
// ack (FR-013). A post-action screenshot is attached when the bound window can
// be captured (FR-007).
func (a *App) handleInboundOperation(sessionID string, parts []*game.Part) error {
	result := a.executeAgentOperation(parts)

	resultFrameID, err := randomHex(8)
	if err != nil {
		a.logger.Error("backend", "handleInboundOperation: frame id failed", map[string]any{
			"session_id": sessionID,
			"tool_id":    result.GetToolId(),
			"error":      err.Error(),
		})
		return fmt.Errorf("send user turn: operation result: %w", err)
	}

	resultFrame := &game.AgentFrame{
		SessionId:  sessionID,
		FrameId:    resultFrameID,
		CreateTime: timestamppb.Now(),
		Sender:     game.FrameSender_FRAME_SENDER_USER,
		Payload: &game.AgentFrame_Content{
			Content: &game.PartBlock{
				Parts: []*game.Part{
					{Kind: &game.Part_ToolResult{ToolResult: result}},
				},
			},
		},
	}

	if err := a.ws.SendFrame(a.ctx, resultFrame); err != nil {
		a.logger.Error("backend", "handleInboundOperation: send failed", map[string]any{
			"session_id": sessionID,
			"tool_id":    result.GetToolId(),
			"error":      err.Error(),
		})
		return fmt.Errorf("send user turn: operation result: %w", err)
	}
	// Mirror the result into the chatstream so the local user sees the tool
	// outcome live; without this the result only reappears via SeedFromHistory
	// after a session restart, since the agent — not the desktop — persists it.
	a.chatStreams.Append(sessionID, resultFrame)
	return nil
}

// groupOperationPartsByToolID collects the operation-bearing parts (mouse
// move, mouse click, key press) from a content PartBlock and groups them by
// tool_id, preserving first-appearance order. Non-operation parts (text,
// image, thinking, tool-result) are ignored. Each group is one atomic desktop
// operation producing one ToolResultPart (data-model.md §5c).
//
// One bridge dispatch stamps every part in its PartBlock with the same
// tool_id, so a saolei WINDOW_MESSAGE block [MouseMovePart, MouseClickPart]
// forms one group; the existing single-part mouse tools form one-element
// groups. Grouping by tool_id generalizes both and stays correct if a frame
// ever carries multiple tool_ids.
func groupOperationPartsByToolID(parts []*game.Part) [][]*game.Part {
	groups := make(map[string][]*game.Part)
	var order []string
	for _, part := range parts {
		toolID := operationToolID(part)
		if toolID == "" {
			continue
		}
		if _, ok := groups[toolID]; !ok {
			order = append(order, toolID)
		}
		groups[toolID] = append(groups[toolID], part)
	}
	result := make([][]*game.Part, 0, len(order))
	for _, id := range order {
		result = append(result, groups[id])
	}
	return result
}

// operationToolID returns the tool_id carried by an operation Part
// (MouseMovePart / MouseClickPart / KeyPart), or "" for non-operation parts.
func operationToolID(part *game.Part) string {
	if part == nil {
		return ""
	}
	if m := part.GetMouseMove(); m != nil {
		return m.GetToolId()
	}
	if c := part.GetMouseClick(); c != nil {
		return c.GetToolId()
	}
	if k := part.GetKeyPress(); k != nil {
		return k.GetToolId()
	}
	return ""
}

// mouseDelivery resolves the InputDelivery for a mouse operation group. Per
// contracts/input-delivery.md §1, all parts in a block SHOULD share the same
// delivery; the click part is authoritative when present, otherwise the move
// part. Either way unset collapses to SIMULATE via operation.IsWindowMessage.
func mouseDelivery(move *game.MouseMovePart, click *game.MouseClickPart) game.InputDelivery {
	if click != nil {
		return click.GetDelivery()
	}
	if move != nil {
		return move.GetDelivery()
	}
	return game.InputDelivery_INPUT_DELIVERY_UNSPECIFIED
}

// executeAgentOperation runs one atomic tool-request group (the operation
// Parts in a PartBlock sharing a tool_id) and returns the matching
// ToolResultPart. The group is realized per the declared InputDelivery
// (contracts/input-delivery.md §4):
//
//   - KeyPart → PostMessage WM_KEYDOWN/WM_KEYUP (no cursor involvement).
//   - WINDOW_MESSAGE mouse → PostMessage WM_*BUTTON* at the client coordinate
//     carried by the companion MouseMovePart in the group; the OS cursor is
//     never moved (occlusion-free, FR-014/SC-003). Requires a MouseMovePart
//     (coordinate source) and a MouseClickPart (action) in the group.
//   - SIMULATE mouse (default) → the existing physical-cursor path: a move
//     part repositions the cursor at screen-absolute coords; a click part
//     dispatches button events at the current position. A combined
//     move+click group moves then clicks.
//
// After the action phase — regardless of whether it succeeded — a follow-up
// screenshot of the bound window is captured (FR-007). The screenshot is
// attached to the result part when capture and sizing succeed; otherwise
// the capture failure is recorded in the result message. Status always
// reflects the ACTION outcome (never SUCCEEDED when the action failed).
// Precondition failures (no operation part, no window bound) return early
// since no screenshot is possible without a bound window.
func (a *App) executeAgentOperation(parts []*game.Part) *game.ToolResultPart {
	toolID := ""
	if len(parts) > 0 {
		toolID = operationToolID(parts[0])
	}

	corrSuffix, err := randomHex(8)
	corrID := "corr-unknown"
	if err != nil {
		a.logger.Error("backend", "executeAgentOperation: correlation id failed", map[string]any{"error": err.Error()})
	} else {
		corrID = "corr-" + corrSuffix
	}

	failed := func(msg string) *game.ToolResultPart {
		a.logger.Error("backend", "executeAgentOperation: failed", map[string]any{
			"tool_id":        toolID,
			"correlation_id": corrID,
			"error":          msg,
		})
		return &game.ToolResultPart{
			ToolId:  toolID,
			Status:  game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED,
			Message: msg,
		}
	}

	if len(parts) == 0 {
		return failed("unsupported operation: no operation part")
	}

	if a.boundWin.Handle == 0 {
		return failed("no window bound")
	}

	// Partition the group by part kind so a WINDOW_MESSAGE move+click combo
	// is realized as one atomic operation.
	var move *game.MouseMovePart
	var click *game.MouseClickPart
	var keyPart *game.KeyPart
	for _, p := range parts {
		if m := p.GetMouseMove(); m != nil {
			move = m
		}
		if c := p.GetMouseClick(); c != nil {
			click = c
		}
		if k := p.GetKeyPress(); k != nil {
			keyPart = k
		}
	}

	// Action phase: accumulate errors instead of early-returning so the
	// screenshot phase always runs (FR-007). actionStatus reflects only the
	// ACTION outcome; a failed action never reports SUCCEEDED.
	var actionErr error
	var screenX, screenY int32
	var bounds capture.WindowBounds
	var actionLabel string

	switch {
	case keyPart != nil:
		actionLabel = fmt.Sprintf("key:%s", keyPart.GetKey().String())
		if eErr := operation.ExecuteKeyMessage(a.boundWin.Handle, keyPart.GetKey()); eErr != nil {
			actionErr = fmt.Errorf("key action: %w", eErr)
		}
	case operation.IsWindowMessage(mouseDelivery(move, click)):
		// Occlusion-free PostMessage path (FR-014, SC-003): no SetCursorPos,
		// no SendInput. The MouseMovePart supplies the client coordinate and
		// the MouseClickPart supplies the action (contracts/input-delivery.md
		// §4 — a WINDOW_MESSAGE click MUST be accompanied by a coordinate
		// source in the same block).
		if click == nil {
			actionErr = fmt.Errorf("window message delivery requires a MouseClickPart action in the same block")
		} else if move == nil {
			actionErr = fmt.Errorf("window message click requires a coordinate source (MouseMovePart) in the same block")
		} else {
			actionLabel = fmt.Sprintf("window-message:%s", click.GetClick().String())
			screenX = move.GetXPx()
			screenY = move.GetYPx()
			if eErr := operation.ExecuteWindowMessageMouse(a.boundWin.Handle, move.GetXPx(), move.GetYPx(), click.GetClick()); eErr != nil {
				actionErr = fmt.Errorf("window message click: %w", eErr)
			}
		}
	default:
		// SIMULATE (default, existing physical-cursor path). A move part
		// captures the bound window's bounds to translate the
		// screenshot-relative target into screen-absolute coordinates, then
		// moves the cursor. A click part dispatches button events at the
		// cursor's current position (the position left by a preceding move).
		if move != nil {
			actionLabel = "move"
			var bErr error
			bounds, bErr = capture.CaptureWindowBounds(a.boundWin.Handle)
			if bErr != nil {
				actionErr = fmt.Errorf("capture window bounds: %w", bErr)
			} else {
				var cErr error
				screenX, screenY, cErr = operation.ScreenshotToScreenCoords(move.GetXPx(), move.GetYPx(), int32(bounds.Left), int32(bounds.Top))
				if cErr != nil {
					actionErr = fmt.Errorf("coordinate conversion: %w", cErr)
				} else if eErr := operation.MoveCursor(screenX, screenY); eErr != nil {
					actionErr = fmt.Errorf("move cursor: %w", eErr)
				}
			}
		}
		if click != nil && actionErr == nil {
			// Synthetic clicks (SendInput) are consumed by Windows for window
			// activation when the target is not the foreground window, so the
			// bound window must be foreground before the button event fires —
			// otherwise the click lands as an activation gesture with no
			// application-level effect. The cursor position from the preceding
			// mouse_move is preserved by SetForeground.
			actionLabel = click.GetClick().String()
			fgBefore := capture.ForegroundWindow()
			fgOk := capture.SetForeground(a.boundWin.Handle)
			fgAfter := capture.ForegroundWindow()
			a.logger.Info("backend", "click: foreground state", map[string]any{
				"tool_id":           toolID,
				"correlation_id":    corrID,
				"window_handle":     a.boundWin.Handle,
				"window_title":      a.boundWin.Title,
				"foreground_before": fgBefore,
				"set_foreground_ok": fgOk,
				"foreground_after":  fgAfter,
			})
			if eErr := operation.ExecuteClickAtCurrentPos(click.GetClick()); eErr != nil {
				actionErr = fmt.Errorf("click action: %w", eErr)
			}
		}
	}

	actionStatus := game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED
	actionMsg := "ok"
	if actionErr != nil {
		actionStatus = game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED
		actionMsg = actionErr.Error()
		a.logger.Error("backend", "executeAgentOperation: action failed", map[string]any{
			"tool_id":        toolID,
			"correlation_id": corrID,
			"error":          actionErr.Error(),
		})
	} else {
		a.logger.Info("backend", "Operation executed", map[string]any{
			"tool_id":       toolID,
			"action":        actionLabel,
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
	result := &game.ToolResultPart{
		ToolId:  toolID,
		Status:  actionStatus,
		Message: actionMsg,
	}

	if a.boundWin.Handle != 0 {
		// Wait briefly so the target window can render the effect of the
		// action before the screenshot is captured.
		time.Sleep(postActionScreenshotDelay)

		capturedImg, captureErr := capture.CaptureWindow(a.ctx, a.boundWin.Handle)
		switch {
		case captureErr != nil:
			result.Message = fmt.Sprintf("%s (screenshot capture failed: %s)", result.Message, captureErr.Error())
		case len(capturedImg.Data) > maxScreenshotBytes:
			result.Message = fmt.Sprintf("%s (screenshot exceeds 5 MiB limit)", result.Message)
		default:
			result.Screenshot = &game.ImagePart{
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

	// Application-level probe: send a status signal and wait for any response.
	// This verifies the full path: desktop → gateway → proxy → agent.
	probeFrameID := "connect-probe-" + corrID[len("corr-"):]
	probeFrame := &game.AgentFrame{
		SessionId:  sessionID,
		FrameId:    probeFrameID,
		CreateTime: timestamppb.Now(),
		Payload: &game.AgentFrame_Status{
			Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE},
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

// OpenChatStream opens (or reopens) the chat push channel for sessionID.
//
// On first access the stream is created and seeded synchronously from
// ListMessages (F11: history fits in memory for a single-session desktop
// client; a very large history may block entry — acceptable for the
// current scope). On re-entry the existing stream is reused without
// re-seeding, but RotateToken is called so any stale EventSource from a
// previous entry is invalidated (R11) and the handoff always carries a
// fresh, non-empty token.
//
// The frontend MUST call CloseChatStream(sessionID) AFTER closeAgent()
// returns on session leave (F5 ordering): closeAgent closes the WS and
// waits on recvDone, so recvLoop has already exited by the time the log
// is dropped.
func (a *App) OpenChatStream(sessionID string) (*ChatStreamHandoff, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("open chat stream: sessionID is empty")
	}
	a.ensureClient()
	if a.chatStreams == nil || a.chatServer == nil {
		return nil, fmt.Errorf("open chat stream: server not started")
	}

	stream, err := a.chatStreams.Open(sessionID, func() ([]*game.Message, error) {
		resp, err := a.client.ListMessages(a.ctx, sessionID)
		if err != nil {
			return nil, err
		}
		return resp.GetMessages(), nil
	})
	if err != nil {
		return nil, fmt.Errorf("open chat stream: %w", err)
	}

	// C3/C10: RotateToken on EVERY call — first creation and re-entry —
	// so old subscribers are disconnected and the handoff always carries a
	// fresh token.
	token := stream.RotateToken()

	return &ChatStreamHandoff{
		Endpoint:    a.chatServer.Endpoint(),
		Token:       token,
		LastEventID: stream.LastID(),
	}, nil
}

// CloseChatStream closes the chat push channel for sessionID. It is
// idempotent (F5: safe to call on an already-closed or never-opened
// stream). The caller MUST close the agent first so recvLoop has exited
// before the event log is dropped (F5 ordering).
func (a *App) CloseChatStream(sessionID string) error {
	if a.chatStreams == nil {
		return nil
	}
	a.chatStreams.Close(sessionID)
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
