package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// debugHoldTimeout is the maximum wall-clock wait for a user to confirm a
// held tool result in debug mode (FR-013 auto-continue). It is a package-level
// var (not const) so unit tests can override it to exercise the timeout branch
// quickly. See specs/022-desktop-debug-mode spec.md FR-013, research.md D4.
var debugHoldTimeout = 15 * time.Minute

// emitDebugEvent emits a Wails runtime event Go→frontend. It is a package-level
// variable so unit tests can replace it with a no-op or recorder: the real
// Wails runtime is unavailable in tests (runtime.EventsEmit calls log.Fatalf
// when the context lacks the Wails "events" value — third_party/.../runtime.go).
// See specs/022-desktop-debug-mode/contracts/debug-control-plane.md §2.
var emitDebugEvent = runtime.EventsEmit

// listWindows is the capture.ListWindows function, exposed as a package-level
// variable so unit tests can override it to inject a mock window list (the
// real capture.ListWindows returns "not supported" on the Linux test host).
// resolveSelectedWindow calls it to turn the selected handle into a WindowRef
// at use time (specs/025-desktop-image-state-refine/contracts/window-select-contract.md §2.3).
var listWindows = capture.ListWindows

// clickSummary maps a MouseClickAction proto enum to a short localized label
// for the debug drawer summary line
// (specs/023-saolei-mcp-refine/contracts/debug-drawer-contract.md §2). The
// proto enum name (e.g. "MOUSE_CLICK_ACTION_LEFT_CLICK") is also carried
// verbatim in `details` for richer/programmatic rendering.
func clickSummary(c game.MouseClickAction) string {
	switch c {
	case game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_CLICK:
		return "左键"
	case game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_DOUBLE_CLICK:
		return "左键双击"
	case game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_CLICK:
		return "右键"
	case game.MouseClickAction_MOUSE_CLICK_ACTION_RIGHT_DOUBLE_CLICK:
		return "右键双击"
	case game.MouseClickAction_MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS:
		return "左右键同按"
	default:
		return "未指定"
	}
}

// methodSummary maps a MouseInputMethod proto enum to a short localized label.
// UNSPECIFIED collapses to SIMULATED on the desktop execution path
// (game.proto comment on MouseInputMethod), so the label reflects that.
func methodSummary(m game.MouseInputMethod) string {
	switch m {
	case game.MouseInputMethod_MOUSE_INPUT_METHOD_SIMULATED:
		return "模拟"
	case game.MouseInputMethod_MOUSE_INPUT_METHOD_WINDOW_MESSAGE:
		return "窗口消息"
	default:
		return "未指定(→模拟)"
	}
}

// keySummary maps a KeyboardKey proto enum to a short UI label by stripping the
// KEYBOARD_KEY_ prefix; unknown values fall back to the proto enum name.
func keySummary(k game.KeyboardKey) string {
	const prefix = "KEYBOARD_KEY_"
	name := k.String()
	if len(name) > len(prefix) && name[:len(prefix)] == prefix {
		return name[len(prefix):]
	}
	return name
}

// describeFlowPart builds a heldOperation descriptor for the operation a
// FlowPart carries (specs/023-saolei-mcp-refine/contracts/debug-drawer-contract.md
// §2). kind is the FlowPart variant name (snake_case, matching the proto field
// names); summary is a localized single-line description built from the
// variant fields; details carries the raw operation fields for optional richer
// rendering/debugging. The function ALWAYS returns a non-nil descriptor: a
// FlowPart carrying no operation kind (e.g. a signal kind that should never
// reach this path) yields a default "unknown" descriptor, so callers never
// need a nil check (style/golang.md §函数参数与返回值: pointer return,
// non-nil guarantee).
func describeFlowPart(part *game.FlowPart) *heldOperation {
	switch {
	case part.GetMouseMove() != nil:
		m := part.GetMouseMove()
		return &heldOperation{
			kind:    "mouse_move",
			summary: fmt.Sprintf("移动光标 (%d, %d) · %s", m.GetXPx(), m.GetYPx(), methodSummary(m.GetMethod())),
			details: map[string]any{
				"xPx":    m.GetXPx(),
				"yPx":    m.GetYPx(),
				"method": m.GetMethod().String(),
			},
		}
	case part.GetMouseClick() != nil:
		c := part.GetMouseClick()
		return &heldOperation{
			kind:    "mouse_click",
			summary: fmt.Sprintf("%s点击 · %s", clickSummary(c.GetClick()), methodSummary(c.GetMethod())),
			details: map[string]any{
				"click":  c.GetClick().String(),
				"method": c.GetMethod().String(),
			},
		}
	case part.GetKeyboardPress() != nil:
		k := part.GetKeyboardPress()
		return &heldOperation{
			kind:    "keyboard_press",
			summary: fmt.Sprintf("按键 %s", keySummary(k.GetKey())),
			details: map[string]any{
				"key": k.GetKey().String(),
			},
		}
	case part.GetMouseMoveAndClick() != nil:
		mc := part.GetMouseMoveAndClick()
		return &heldOperation{
			kind:    "mouse_move_and_click",
			summary: fmt.Sprintf("移动并点击 (%d, %d) · %s · %s", mc.GetXPx(), mc.GetYPx(), clickSummary(mc.GetClick()), methodSummary(mc.GetMethod())),
			details: map[string]any{
				"xPx":    mc.GetXPx(),
				"yPx":    mc.GetYPx(),
				"click":  mc.GetClick().String(),
				"method": mc.GetMethod().String(),
			},
		}
	}
	// Non-operation FlowPart (signal kind, or empty): the caller still gets a
	// usable descriptor so a Confirm control always renders. recvLoop only
	// routes operation FlowParts to handleInboundOperation, so this branch is
	// defensive.
	return &heldOperation{
		kind:    "unknown",
		summary: "未知操作",
		details: map[string]any{},
	}
}

// hold represents a pending tool-result hold in debug mode. One instance per
// held result, keyed by tool_id. The result frame itself stays in the
// handleInboundOperation stack frame; the hold only carries the release signal.
// See specs/022-desktop-debug-mode/data-model.md "HeldToolResult".
type hold struct {
	toolID        string
	confirmCh     chan struct{}
	releaseReason string // set by the signalling side under holdsMu before closing confirmCh
}

// heldOperation describes the operation a held result corresponds to, so the
// session-top debug drawer can render a human-readable request line without
// proto knowledge (specs/023-saolei-mcp-refine/contracts/debug-drawer-contract.md
// §2). The Go backend builds it from the FlowPart the desktop received and
// executed. It is purely an operation-channel artifact (decoupled from the
// conversation render path — research.md D10/D11).
type heldOperation struct {
	kind    string
	summary string
	details map[string]any
}

// close releases the hold: it sets releaseReason so the blocked caller can
// report why it woke, then closes confirmCh to unblock the select in
// holdAndRelease. The caller MUST hold holdsMu and MUST delete the entry from
// the holds map afterwards (which prevents a double-close on a second signal).
// See specs/022-desktop-debug-mode/data-model.md "HeldToolResult" state transitions.
func (h *hold) close(reason string) {
	h.releaseReason = reason
	close(h.confirmCh)
}

// confirmed is the confirm-scoped release entry point: it calls close with the
// fixed reason "confirmed" (contracts/debug-control-plane.md §1.2). The same
// holdsMu / map-delete invariants apply as for close.
func (h *hold) confirmed() {
	h.close("confirmed")
}

// App is the Wails application struct holding all state.
type App struct {
	logger       *applog.Logger
	client       *api.Client
	ws           *api.WSClient
	cfg          api.Config
	ctx          context.Context
	selectedMu   sync.Mutex
	selectedWin  uintptr // handle of the selected window; 0 = none (spec 025 FR-006)
	sessionID    string  // active session set on WebSocket connect
	recvDone     chan struct{}
	chatStreams  *chatstream.Registry
	chatServer   *chatstream.Server
	debugEnabled atomic.Bool
	holds        map[string]*hold // active debug-mode holds keyed by tool_id
	holdsMu      sync.Mutex       // guards holds
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

// SetDebugMode enables or disables desktop debug mode (Wails-bound;
// contracts/debug-control-plane.md §1.1). It mirrors the flag atomically on
// *App, propagates it to the applog DEBUG gate, and logs the transition. When
// disabling it immediately releases every currently-held tool result
// (reason "debug-off") so no turn is left blocked (spec FR, Edge Case
// "Debug toggled OFF mid-hold"). Idempotent.
func (a *App) SetDebugMode(enabled bool) error {
	a.debugEnabled.Store(enabled)
	a.logger.SetDebug(enabled)
	if !enabled {
		a.releaseAllHolds("debug-off")
	}
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	a.logger.Info("backend", "debug mode "+state)
	return nil
}

// ConfirmToolResult releases the held tool result identified by toolID
// (Wails-bound; contracts/debug-control-plane.md §1.2), causing the blocked
// handleInboundOperation to send it to the agent. It is a logged no-op
// (returns nil) if toolID is not currently held — e.g., the 15-minute
// auto-continue already released it, or debug mode was turned off. The
// signalling side sets releaseReason and closes confirmCh under holdsMu, then
// deletes the entry; this avoids a double-close panic on a second signal.
func (a *App) ConfirmToolResult(toolID string) error {
	a.holdsMu.Lock()
	h, ok := a.holds[toolID]
	if ok {
		h.confirmed()
		delete(a.holds, toolID)
	}
	a.holdsMu.Unlock()
	if !ok {
		a.logger.Debug("backend", "ConfirmToolResult: no active hold for tool", map[string]any{
			"tool_id": toolID,
		})
	}
	return nil
}

// releaseAllHolds closes every active hold's confirmCh with the given reason
// and clears the map. Called by SetDebugMode(false) (reason "debug-off") so no
// turn is left blocked when debug mode is turned off. The blocked
// handleInboundOperation callers wake, read the reason, and proceed to
// ws.SendFrame (contract §1.1 side effects).
func (a *App) releaseAllHolds(reason string) {
	a.holdsMu.Lock()
	for toolID, h := range a.holds {
		h.close(reason)
		delete(a.holds, toolID)
	}
	a.holdsMu.Unlock()
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
			McpNames:     req.McpNames,
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
// screenshotData is the raw PNG bytes of the selected window; pass an empty
// slice when no screenshot is attached. screenshotWidth and screenshotHeight
// describe the pixel dimensions of screenshotData and are ignored when it is
// empty.
//
// The user turn is carried as a messageParts frame whose MessageParts holds a
// text MessagePart and, when a screenshot is attached, an image MessagePart.
// Inbound FlowParts operations (mouse/keyboard) are auto-executed by recvLoop
// and a matching FlowResultPart is sent back over the same WebSocket connection
// on the control channel (FR-013; spec 025 FR-023/FR-024). The result part
// carries a post-action screenshot of the selected window (FR-007).
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

	parts := []*game.MessagePart{
		{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: text}}},
	}
	if len(screenshotData) > 0 {
		// The screenshot was captured from the selected window; resolve it to
		// attach ScaleFactor/WindowTitle (spec 025 FR-003/FR-006). No selection
		// is a graceful failure (FR-005).
		win, err := a.resolveSelectedWindow()
		if err != nil {
			return fmt.Errorf("send user turn: %w", err)
		}
		parts = append(parts, &game.MessagePart{
			Kind: &game.MessagePart_Image{Image: &game.ImagePart{
				Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
				Data:        screenshotData,
				WidthPx:     int32(screenshotWidth),
				HeightPx:    int32(screenshotHeight),
				ScaleFactor: win.ScaleFactor,
				WindowTitle: win.Title,
			}},
		})
	}

	frame := &game.AgentFrame{
		SessionId:        sessionID,
		FrameId:          frameID,
		CreateTime:       timestamppb.Now(),
		Sender:           game.FrameSender_FRAME_SENDER_USER,
		AgentProfileName: agentProfileName,
		Payload: &game.AgentFrame_MessageParts{
			MessageParts: &game.MessageParts{Parts: parts},
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
// appends each display/control frame to the session's chat stream as needed.
// It runs in its own goroutine launched by SendUserTurn. The loop terminates —
// and closes recvDone — when a wait signal is received (the agent is done) or
// RecvFrame errors.
//
// A frame carries exactly one payload: a batch of display blocks (MessageParts)
// OR a batch of control blocks (FlowParts) (content-model split, spec 023 C3).
//   - messageParts: appended to the chat stream for the frontend to render
//     (text/thinking/image/tool_call/tool_result).
//   - flowParts: operation kinds (mouse/keyboard) are executed via
//     handleInboundOperation and are NOT appended to the chat stream (FR-005:
//     operations never render as conversation entries); signal kinds
//     (wait/warn/status) ARE appended so the frontend can react (wait clears
//     the typing indicator, warn shows a warning, status is a no-op for chat).
//
// On RecvFrame error a synthesized wait FlowPart is appended so the frontend
// can settle the turn before the failure surfaces (data-model.md §9).
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
				Payload: &game.AgentFrame_FlowParts{
					FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
						{Kind: &game.FlowPart_Wait{Wait: &game.WaitSignal{}}},
					}},
				},
			})
			return
		}
		frameCount++

		a.logger.Debug("backend", "inbound frame received", map[string]any{
			"session_id":  sessionID,
			"frame_count": frameCount,
		})

		switch payload := resp.GetPayload().(type) {
		case *game.AgentFrame_MessageParts:
			// Display channel: render in the conversation.
			a.chatStreams.Append(sessionID, resp)
		case *game.AgentFrame_FlowParts:
			for _, fp := range payload.FlowParts.GetParts() {
				// Operation kinds drive desktop execution and are never
				// conversation entries (FR-005). Signal kinds are forwarded to
				// the chat stream so the frontend can react (wait/warn/status).
				if fp.GetMouseMove() != nil || fp.GetMouseClick() != nil ||
					fp.GetKeyboardPress() != nil || fp.GetMouseMoveAndClick() != nil {
					if err := a.handleInboundOperation(sessionID, fp); err != nil {
						a.logger.Error("backend", "recvLoop: handle inbound operation failed", map[string]any{
							"session_id":  sessionID,
							"frame_count": frameCount,
							"error":       err.Error(),
						})
						return
					}
					continue
				}
				// Signal FlowPart (wait/warn/status): append so the frontend
				// reacts; not rendered as a chat bubble by ChatView.
				a.chatStreams.Append(sessionID, &game.AgentFrame{
					SessionId:  resp.GetSessionId(),
					FrameId:    resp.GetFrameId(),
					CreateTime: resp.GetCreateTime(),
					Sender:     resp.GetSender(),
					Payload: &game.AgentFrame_FlowParts{
						FlowParts: &game.FlowParts{Parts: []*game.FlowPart{fp}},
					},
				})
				if fp.GetWait() != nil {
					a.logger.Info("backend", "recvLoop: done", map[string]any{
						"session_id":  sessionID,
						"frame_count": frameCount,
					})
					return
				}
			}
		}
	}
}

// handleInboundOperation executes an inbound tool-request FlowPart
// (MouseMovePart/MouseClickPart/KeyboardPressPart/MouseMoveAndClickPart) and
// sends the matching FlowResultPart back over the WebSocket wrapped in a
// flowParts frame (the control channel). The result part carries the same
// tool_id and a SUCCEEDED/FAILED status. A post-action screenshot is attached
// when the bound window can be captured.
//
// Per spec 025 FR-023/FR-024 the operation outcome travels as a FlowResultPart
// (a FlowPart kind) on the control channel, NOT as a display tool_result
// MessagePart. Per spec 023 FR-010/C8 the result is NOT mirrored into the chat
// stream: the screenshot the conversation shows comes from the agent's later
// tool_result MessagePart (the LLM tool result), not a desktop-side mirror. The
// desktop returns the result only over the WS (resolving the agent's dispatch).
//
// Debug mode reorders the result-return boundary
// (specs/022-desktop-debug-mode spec.md FR-006/FR-007/FR-011, data-model.md
// state machine, research.md D4):
//
//   - Debug OFF: compute → send (FR-011 — no events).
//   - Debug ON:  compute → register hold → emit game:debug:result-held
//     (payload EXTENDED with the operation descriptor —
//     specs/023-saolei-mcp-refine/contracts/debug-drawer-contract.md §2/§4) →
//     block on confirm/15-min/shutdown → emit game:debug:result-released →
//     send. The sent frame is identical to the OFF path (FR-007 transparency).
func (a *App) handleInboundOperation(sessionID string, part *game.FlowPart) error {
	result := a.executeAgentOperation(part)

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
		Payload: &game.AgentFrame_FlowParts{
			FlowParts: &game.FlowParts{
				Parts: []*game.FlowPart{
					{Kind: &game.FlowPart_FlowResult{FlowResult: result}},
				},
			},
		},
	}

	toolID := result.GetToolId()
	if a.debugEnabled.Load() {
		// holdAndRelease builds the drawer descriptor from `part` internally
		// (contracts/debug-drawer-contract.md §4) — handleInboundOperation
		// only owns execute → hold → send, not the descriptor shape.
		a.holdAndRelease(toolID, part)
		if err := a.ws.SendFrame(a.ctx, resultFrame); err != nil {
			a.logger.Error("backend", "handleInboundOperation: send failed", map[string]any{
				"session_id": sessionID,
				"tool_id":    toolID,
				"error":      err.Error(),
			})
			return fmt.Errorf("send user turn: operation result: %w", err)
		}
		return nil
	}

	if err := a.ws.SendFrame(a.ctx, resultFrame); err != nil {
		a.logger.Error("backend", "handleInboundOperation: send failed", map[string]any{
			"session_id": sessionID,
			"tool_id":    toolID,
			"error":      err.Error(),
		})
		return fmt.Errorf("send user turn: operation result: %w", err)
	}
	return nil
}

// holdAndRelease is the debug-mode hold boundary: it registers a hold for
// toolID, emits game:debug:result-held (carrying the operation descriptor so
// the session-top drawer can render the request content —
// specs/023-saolei-mcp-refine/contracts/debug-drawer-contract.md §2/§4),
// blocks until the hold is released (confirm / 15-min auto-continue /
// shutdown), emits game:debug:result-released, and returns the release reason.
// handleInboundOperation calls it between Append and SendFrame when debug
// mode is ON.
//
// The operation descriptor for the emit payload is built INSIDE this function
// by calling describeFlowPart(part) as an adapter that formats the raw
// FlowPart into a drawer-renderable shape. holdAndRelease owns the emit
// concern end-to-end; handleInboundOperation is unaware of the descriptor.
// describeFlowPart always returns non-nil, so no nil check is needed here.
//
// part is the inbound FlowPart the desktop received and executed; it is only
// read (to build the descriptor), never mutated. Pointer param per
// style/golang.md §函数参数与返回值.
//
// The select arms map to the data-model state machine
// (specs/022-desktop-debug-mode/data-model.md):
//
//   - <-confirmCh: released by ConfirmToolResult ("confirmed") or
//     SetDebugMode(false) ("debug-off"); releaseReason was set by the
//     signalling side under holdsMu.
//   - <-time.After(debugHoldTimeout): 15-min auto-continue (FR-013, "timeout").
//   - <-a.ctx.Done(): app/session shutdown ("shutdown").
//
// The delete after the select is idempotent: the confirm/debug-off branch was
// already deleted by the signalling side, while the timeout/shutdown branch
// still holds the entry and needs removal. See contracts §2.2.
func (a *App) holdAndRelease(toolID string, part *game.FlowPart) string {
	op := describeFlowPart(part)

	a.holdsMu.Lock()
	h := &hold{
		toolID:    toolID,
		confirmCh: make(chan struct{}),
	}
	if a.holds == nil {
		a.holds = map[string]*hold{}
	}
	a.holds[toolID] = h
	a.holdsMu.Unlock()

	emitDebugEvent(a.ctx, "game:debug:result-held", map[string]any{
		"toolId": toolID,
		"operation": map[string]any{
			"kind":    op.kind,
			"summary": op.summary,
			"details": op.details,
		},
	})

	var reason string
	select {
	case <-h.confirmCh:
		// releaseReason was set under holdsMu by the signalling side before
		// closing the channel; read it under the same lock to be explicit
		// about the memory model.
		a.holdsMu.Lock()
		reason = h.releaseReason
		a.holdsMu.Unlock()
	case <-time.After(debugHoldTimeout):
		reason = "timeout"
	case <-a.ctx.Done():
		reason = "shutdown"
	}

	emitDebugEvent(a.ctx, "game:debug:result-released", map[string]any{
		"toolId": toolID,
		"reason": reason,
	})
	a.holdsMu.Lock()
	delete(a.holds, toolID)
	a.holdsMu.Unlock()

	return reason
}

// executeAgentOperation runs an inbound tool-request FlowPart via the
// appropriate executor and returns the matching FlowResultPart (the
// control-channel operation outcome, spec 025 FR-023/FR-024). The FlowPart
// kinds handled are: MouseMovePart, MouseClickPart, KeyboardPressPart, and
// MouseMoveAndClickPart. Each mouse Part carries a MouseInputMethod that
// selects the desktop execution path (spec 018-saolei-mcp FR-004c):
//
//   - SIMULATED (the default, including UNSPECIFIED) is the existing
//     behavior: screenshot-relative coords are converted to screen-absolute
//     via the selected window's bounds, the OS cursor is repositioned with
//     SetCursorPos, and button events are dispatched via SendInput.
//   - WINDOW_MESSAGE posts WM_* messages to the selected window's HWND with
//     window-client coordinates packed into lParam and does NOT move the OS
//     cursor (FR-004d).
//
// KeyboardPressPart is method-agnostic: it posts WM_KEYDOWN/WM_KEYUP to the
// selected HWND (FR-004a).
//
// screenshot of the selected window is captured (FR-007). The screenshot is
// attached to the result part when capture and sizing succeed; otherwise
// the capture failure is recorded in the result message. Status always
// reflects the ACTION outcome (never SUCCEEDED when the action failed).
// Precondition failures (no tool payload, no window selected) return early
// since no screenshot is possible without a selected window.
func (a *App) executeAgentOperation(part *game.FlowPart) *game.FlowResultPart {
	move := part.GetMouseMove()
	click := part.GetMouseClick()
	keyboard := part.GetKeyboardPress()
	moveClick := part.GetMouseMoveAndClick()
	var toolID string
	switch {
	case move != nil:
		toolID = move.GetToolId()
	case click != nil:
		toolID = click.GetToolId()
	case keyboard != nil:
		toolID = keyboard.GetToolId()
	case moveClick != nil:
		toolID = moveClick.GetToolId()
	}

	corrSuffix, err := randomHex(8)
	corrID := "corr-unknown"
	if err != nil {
		a.logger.Error("backend", "executeAgentOperation: correlation id failed", map[string]any{"error": err.Error()})
	} else {
		corrID = "corr-" + corrSuffix
	}

	failed := func(msg string) *game.FlowResultPart {
		a.logger.Error("backend", "executeAgentOperation: failed", map[string]any{
			"tool_id":        toolID,
			"correlation_id": corrID,
			"error":          msg,
		})
		return &game.FlowResultPart{
			ToolId:  toolID,
			Status:  game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED,
			Message: msg,
		}
	}

	if move == nil && click == nil && keyboard == nil && moveClick == nil {
		return failed("unsupported operation: only mouse and keyboard operations are supported")
	}

	// Resolve the selected window (single source of truth, spec 025 FR-006).
	// No selection is a graceful failure (FR-005) replacing the former
	// "no window bound" guard. The handle is resolved fresh on every operation
	// so re-selecting a different window retargets subsequent ops (FR-004).
	win, err := a.resolveSelectedWindow()
	if err != nil {
		return failed(err.Error())
	}
	a.logger.Debug("backend", "resolved selected window", map[string]any{
		"tool_id":        toolID,
		"correlation_id": corrID,
		"window_handle":  win.Handle,
		"window_title":   win.Title,
	})

	a.logger.Debug("backend", "executing tool operation", map[string]any{
		"tool_id":        toolID,
		"correlation_id": corrID,
	})

	// Action phase: accumulate errors instead of early-returning so the
	// screenshot phase always runs (FR-007). actionStatus reflects only the
	// ACTION outcome; a failed action never reports SUCCEEDED.
	//
	// Each Part kind dispatches to the matching executor with the resolved
	// selected window's handle (spec 025 FR-006). Mouse Parts further route on
	// their MouseInputMethod field. WINDOW_MESSAGE mouse ops post WM_* messages
	// to the HWND with window-client coordinates and skip the
	// screenshot-relative → screen-absolute conversion; SIMULATED ops reuse the
	// existing SetCursorPos + SendInput path.
	var actionErr error
	var actionLabel string
	switch {
	case keyboard != nil:
		actionLabel = "keyboard_press:" + keyboard.GetKey().String()
		if eErr := operation.ExecuteKeyboardPress(win.Handle, keyboard.GetKey()); eErr != nil {
			actionErr = fmt.Errorf("keyboard press: %w", eErr)
		}
	case moveClick != nil:
		actionLabel, actionErr = a.runMouseMoveAndClick(moveClick, corrID, win.Handle)
	case move != nil:
		actionLabel, actionErr = a.runMouseMove(move, corrID, win.Handle)
	case click != nil:
		actionLabel, actionErr = a.runMouseClick(click, corrID, win.Handle)
	}

	actionStatus := game.ToolResultStatus_TOOL_RESULT_STATUS_SUCCEEDED
	actionMsg := "ok"
	if actionErr != nil {
		actionStatus = game.ToolResultStatus_TOOL_RESULT_STATUS_FAILED
		actionMsg = actionErr.Error()
	} else {
		a.logger.Info("backend", "Operation executed", map[string]any{
			"tool_id":        toolID,
			"action":         actionLabel,
			"correlation_id": corrID,
		})
	}

	// Single exit: build the result with the accumulated action status, then
	// always attempt a post-action screenshot of the resolved selected window
	// (FR-007). win is non-zero — resolveSelectedWindow already rejected the
	// no-selection precondition — so the screenshot always runs (errors are
	// recorded in the message, never swallowed by an early return).
	result := &game.FlowResultPart{
		ToolId:  toolID,
		Status:  actionStatus,
		Message: actionMsg,
	}

	// Wait briefly so the target window can render the effect of the action
	// before the screenshot is captured.
	time.Sleep(postActionScreenshotDelay)

	capturedImg, captureErr := capture.CaptureWindow(a.ctx, win.Handle)
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
			ScaleFactor: win.ScaleFactor,
			WindowTitle: win.Title,
		}
	}

	a.logger.Debug("backend", "tool operation result", map[string]any{
		"tool_id":        toolID,
		"correlation_id": corrID,
		"status":         result.GetStatus().String(),
	})
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
	windows, err := listWindows(a.ctx)
	if err != nil {
		a.logger.Error("backend", "List windows failed", map[string]any{"error": err.Error(), "correlation_id": corrID})
		return nil, err
	}
	a.logger.Info("backend", "Windows listed", map[string]any{"count": len(windows), "correlation_id": corrID})
	return windows, nil
}

// SetSelectedWindow stores the handle of the window currently selected in the
// desktop session chat dropdown (Wails-bound; exposed to the frontend). The
// selected window is the single source of truth for every screenshot and
// operation — there is no separate "bind" step (spec 025 FR-001/FR-006,
// contracts/window-select-contract.md §2). The WindowRef is resolved from
// this handle at use time via listWindows, so a window closing between
// selection and use surfaces as a graceful use-time failure (FR-005) rather
// than being rejected at selection time.
func (a *App) SetSelectedWindow(hwnd uintptr) error {
	a.selectedMu.Lock()
	a.selectedWin = hwnd
	a.selectedMu.Unlock()
	a.logger.Info("backend", "Selected window set", map[string]any{"hwnd": hwnd})
	return nil
}

// resolveSelectedWindow resolves the currently selected window to a
// capture.WindowRef by looking it up via listWindows (the same lookup the
// former BindWindow performed, spec 025 D3). It returns a graceful error when
// no window is selected or the selected handle is no longer present
// (closed/minimized/hidden between selection and use) — spec 025 FR-005,
// contracts/window-select-contract.md §2.3/§3.
func (a *App) resolveSelectedWindow() (capture.WindowRef, error) {
	a.selectedMu.Lock()
	hwnd := a.selectedWin
	a.selectedMu.Unlock()
	if hwnd == 0 {
		return capture.WindowRef{}, fmt.Errorf("no window selected")
	}
	windows, err := listWindows(a.ctx)
	if err != nil {
		return capture.WindowRef{}, fmt.Errorf("resolve selected window: %w", err)
	}
	for _, w := range windows {
		if w.Handle == hwnd {
			return w, nil
		}
	}
	return capture.WindowRef{}, fmt.Errorf("selected window %d not found (it may have closed)", hwnd)
}

// CaptureScreenshot captures the currently selected window as a PNG image
// (spec 025 FR-003). It resolves the selected window at capture time and
// returns a graceful error when no window is selected (FR-005).
func (a *App) CaptureScreenshot() (*capture.CapturedImage, error) {
	win, err := a.resolveSelectedWindow()
	if err != nil {
		return nil, fmt.Errorf("capture screenshot: %w", err)
	}
	corrSuffix, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("capture screenshot: %w", err)
	}
	corrID := "corr-" + corrSuffix
	// Capture bounds before screenshot for logging.
	bnds, _ := capture.CaptureWindowBounds(win.Handle)
	a.logger.Info("backend", "Capturing screenshot", map[string]any{"hwnd": win.Handle, "correlation_id": corrID})
	img, err := capture.CaptureWindow(a.ctx, win.Handle)
	if err != nil {
		a.logger.Error("backend", "Capture screenshot failed", map[string]any{"error": err.Error(), "correlation_id": corrID})
		return nil, err
	}
	a.logger.Info("backend", "screenshot captured", map[string]any{
		"hwnd":           win.Handle,
		"title":          win.Title,
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
//
// On success the probe response's StatusSignalStatus enum name is returned
// (e.g. "STATUS_SIGNAL_STATUS_IDLE") so the frontend can reconcile its typing
// indicator against the agent's real working state
// (specs/021-agent-session-resync/contracts/agent-desktop-channel-contract.md §1).
func (a *App) ConnectAgent(sessionID string) (string, error) {
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
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
		return "", err
	}

	// Application-level probe: send a status signal and wait for any response.
	// This verifies the full path: desktop → gateway → proxy → agent. Status is
	// now a FlowPart kind (spec 023 C3 / FR-003).
	probeFrameID := "connect-probe-" + corrID[len("corr-"):]
	probeFrame := &game.AgentFrame{
		SessionId:  sessionID,
		FrameId:    probeFrameID,
		CreateTime: timestamppb.Now(),
		Payload: &game.AgentFrame_FlowParts{
			FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
				{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_ACTIVE}}},
			}},
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
		return "", fmt.Errorf("connect session: probe send failed: %w", err)
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
		return "", fmt.Errorf("connect session: probe receive failed: %w", err)
	}

	// Capture the probe response's StatusSignalStatus enum name so the frontend
	// can reconcile its typing indicator. The status rides as a FlowPart kind
	// (spec 023 C3); when the response carries no status FlowPart, the
	// zero-value enum resolves to STATUS_SIGNAL_STATUS_UNSPECIFIED.
	status := "STATUS_SIGNAL_STATUS_UNSPECIFIED"
	if fp, ok := resp.GetPayload().(*game.AgentFrame_FlowParts); ok {
		for _, p := range fp.FlowParts.GetParts() {
			if s := p.GetStatus(); s != nil {
				status = s.GetStatus().String()
				break
			}
		}
	}
	a.logger.Info("backend", "Connect probe succeeded", map[string]any{
		"trace_id":          traceID,
		"session_id":        sessionID,
		"frame_id":          probeFrameID,
		"response_frame_id": resp.GetFrameId(),
		"status":            status,
		"correlation_id":    corrID,
	})

	a.ws = ws
	a.sessionID = sessionID
	a.logger.Info("backend", "Session connected via WebSocket", map[string]any{
		"trace_id":       traceID,
		"session_id":     sessionID,
		"correlation_id": corrID,
	})
	return status, nil
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
