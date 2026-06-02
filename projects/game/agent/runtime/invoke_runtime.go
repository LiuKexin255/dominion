package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"dominion/projects/game/agent/domain"
)

// Operation result status values matching the proto AgentOperationResultStatus enum.
const (
	operationResultAccepted int32 = 1
	operationResultExecuted int32 = 2
	operationResultRejected int32 = 3
	operationResultFailed   int32 = 4
)

// pendingOp tracks the expected operation ID and its sequence for a session.
type pendingOp struct {
	operationID string
}

// InvokeRuntime is an in-memory implementation of domain.Runtime that provides
// a full invoke state machine with profile loading, sequence validation, and
// deterministic mock operation output.
type InvokeRuntime struct {
	mu         sync.Mutex
	agents     map[string]*domain.InvokeContext
	prompt     domain.PromptServiceClient
	counter    int
	pendingOps map[string]*pendingOp
}

// NewInvokeRuntime creates a new InvokeRuntime with the given prompt client.
func NewInvokeRuntime(promptClient domain.PromptServiceClient) *InvokeRuntime {
	return &InvokeRuntime{
		agents:     make(map[string]*domain.InvokeContext),
		prompt:     promptClient,
		pendingOps: make(map[string]*pendingOp),
	}
}

// Create delegates to CreateWithProfile with the "default" profile.
func (r *InvokeRuntime) Create(ctx context.Context, sessionID string) (*domain.Status, error) {
	return r.CreateWithProfile(ctx, sessionID, "default")
}

// CreateWithProfile creates an agent session with the given profile name.
// It validates the profile, its skills, and MCP names against the built-in
// registry before storing the invoke context.
func (r *InvokeRuntime) CreateWithProfile(ctx context.Context, sessionID string, profileName string) (*domain.Status, error) {
	if profileName == "" {
		profileName = "default"
	}

	profile, err := r.prompt.GetProfile(ctx, profileName)
	if err != nil {
		return nil, fmt.Errorf("get profile %q: %w", profileName, err)
	}
	if !profile.Enabled {
		return nil, fmt.Errorf("profile %q is disabled", profileName)
	}

	for _, skillName := range profile.SkillNames {
		skill, err := r.prompt.GetSkill(ctx, skillName)
		if err != nil {
			return nil, fmt.Errorf("get skill %q: %w", skillName, err)
		}
		if !skill.Enabled {
			return nil, fmt.Errorf("skill %q is disabled", skillName)
		}
	}

	for _, mcpName := range profile.MCPNames {
		if !r.isMCPValid(mcpName) {
			return nil, fmt.Errorf("mcp %q is not in the built-in registry", mcpName)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.agents[sessionID] = &domain.InvokeContext{
		SessionID:   sessionID,
		State:       domain.InvokeStateIdle,
		ProfileName: profileName,
		Skills:      profile.SkillNames,
		MCPNames:    profile.MCPNames,
	}

	return &domain.Status{
		SessionId: sessionID,
		Status:    "created",
	}, nil
}

// isMCPValid checks whether the given MCP name is in the built-in registry.
// For step3.a the registry is hardcoded to be empty — no MCPs are valid.
func (r *InvokeRuntime) isMCPValid(mcpName string) bool {
	_ = mcpName
	return false
}

// Delete removes the agent session. It is idempotent: deleting a non-existent
// session returns nil.
func (r *InvokeRuntime) Delete(_ context.Context, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, sessionID)
	delete(r.pendingOps, sessionID)
	return nil
}

// Status returns the current status of the agent session.
func (r *InvokeRuntime) Status(_ context.Context, sessionID string) (*domain.Status, error) {
	r.mu.Lock()
	ictx, ok := r.agents[sessionID]
	r.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("agent %q not found", sessionID)
	}

	return &domain.Status{
		SessionId: sessionID,
		Status:    invokeStateString(ictx.State),
	}, nil
}

// invokeStateString converts an InvokeState to a human-readable string.
func invokeStateString(s domain.InvokeState) string {
	switch s {
	case domain.InvokeStateInvoking:
		return "invoking"
	case domain.InvokeStateWaitingForOperationResult:
		return "waiting_for_operation_result"
	case domain.InvokeStateCompleted:
		return "completed"
	case domain.InvokeStateFailed:
		return "failed"
	default:
		return "idle"
	}
}

// ReceiveScreenshot processes a screenshot and starts or continues an invoke
// cycle. In the mock implementation it returns two frames: a text frame and a
// deterministic center-click operation.
func (r *InvokeRuntime) ReceiveScreenshot(_ context.Context, sessionID string, input *domain.ScreenshotInput) ([]*domain.Frame, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ictx, ok := r.agents[sessionID]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", sessionID)
	}

	switch ictx.State {
	case domain.InvokeStateIdle:
		return r.handleIdleScreenshot(sessionID, input, ictx)
	case domain.InvokeStateInvoking:
		return r.handleInvokingScreenshot(sessionID, input, ictx)
	case domain.InvokeStateWaitingForOperationResult:
		return r.handleWaitingScreenshot(ictx)
	default:
		return nil, fmt.Errorf("cannot receive screenshot in state %v", ictx.State)
	}
}

// handleIdleScreenshot starts a new invoke from the Idle state.
func (r *InvokeRuntime) handleIdleScreenshot(sessionID string, input *domain.ScreenshotInput, ictx *domain.InvokeContext) ([]*domain.Frame, error) {
	r.counter++
	ictx.InvokeID = fmt.Sprintf("invoke-%s-%d", sessionID, r.counter)
	ictx.State = domain.InvokeStateInvoking

	opSeq := ictx.Sequence + 1
	opID := fmt.Sprintf("op-%s-%d", sessionID, opSeq)

	r.pendingOps[sessionID] = &pendingOp{operationID: opID}

	frames := []*domain.Frame{
		{
			Type:    domain.FrameTypeText,
			Content: "analyzing screenshot...",
		},
		{
			Type:         domain.FrameTypeOperation,
			OperationID:  opID,
			ScreenshotID: input.CaptureId,
			OperationSeq: opSeq,
			IsMouse:      true,
			Button:       1,
			ClickType:    1,
			XPx:          input.WidthPx / 2,
			YPx:          input.HeightPx / 2,
		},
	}

	ictx.State = domain.InvokeStateWaitingForOperationResult
	return frames, nil
}

// handleInvokingScreenshot handles a screenshot received while already invoking.
func (r *InvokeRuntime) handleInvokingScreenshot(sessionID string, input *domain.ScreenshotInput, ictx *domain.InvokeContext) ([]*domain.Frame, error) {
	opSeq := ictx.Sequence + 1
	opID := fmt.Sprintf("op-%s-%d", sessionID, opSeq)

	r.pendingOps[sessionID] = &pendingOp{operationID: opID}

	frames := []*domain.Frame{
		{
			Type:    domain.FrameTypeText,
			Content: "analyzing screenshot...",
		},
		{
			Type:         domain.FrameTypeOperation,
			OperationID:  opID,
			ScreenshotID: input.CaptureId,
			OperationSeq: opSeq,
			IsMouse:      true,
			Button:       1,
			ClickType:    1,
			XPx:          input.WidthPx / 2,
			YPx:          input.HeightPx / 2,
		},
	}

	ictx.State = domain.InvokeStateWaitingForOperationResult
	return frames, nil
}

// handleWaitingScreenshot returns a warn frame when a screenshot arrives while
// already waiting for an operation result.
func (r *InvokeRuntime) handleWaitingScreenshot(ictx *domain.InvokeContext) ([]*domain.Frame, error) {
	return []*domain.Frame{
		{
			Type:        domain.FrameTypeWarn,
			WarnMessage: "screenshot received while waiting for operation result",
			WarnCode:    "WRONG_STATE",
		},
	}, nil
}

// ReceiveOperationResult handles the result of a desktop operation, performing
// sequence validation and state transitions.
func (r *InvokeRuntime) ReceiveOperationResult(_ context.Context, sessionID string, result *domain.OperationResult) ([]*domain.Frame, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ictx, ok := r.agents[sessionID]
	if !ok {
		return nil, nil
	}

	if ictx.State != domain.InvokeStateWaitingForOperationResult {
		return []*domain.Frame{
			{Type: domain.FrameTypeWarn, WarnMessage: "operation result received in wrong state", WarnCode: "WRONG_STATE"},
		}, nil
	}

	if result.InvokeID != "" && ictx.InvokeID != "" && result.InvokeID != ictx.InvokeID {
		return []*domain.Frame{
			{Type: domain.FrameTypeWarn, WarnMessage: fmt.Sprintf("invoke ID mismatch: got %q, expected %q", result.InvokeID, ictx.InvokeID), WarnCode: "INVOKE_ID_MISMATCH"},
		}, nil
	}

	// Validate sequence: prefer the explicit Sequence field from the result,
	// falling back to extraction from the operation ID.
	seq := result.Sequence
	if seq == 0 {
		extracted, ok := extractSequence(result.OperationID)
		if !ok {
			return []*domain.Frame{
				{Type: domain.FrameTypeWarn, WarnMessage: fmt.Sprintf("invalid operation ID %q", result.OperationID), WarnCode: "INVALID_OPERATION_ID"},
			}, nil
		}
		seq = extracted
	}

	if seq <= ictx.Sequence {
		return []*domain.Frame{
			{Type: domain.FrameTypeWarn, WarnMessage: fmt.Sprintf("stale sequence %d (current %d)", seq, ictx.Sequence), WarnCode: "STALE_SEQUENCE"},
		}, nil
	}
	if seq > ictx.Sequence+1 {
		return []*domain.Frame{
			{Type: domain.FrameTypeWarn, WarnMessage: fmt.Sprintf("sequence gap: got %d, expected %d", seq, ictx.Sequence+1), WarnCode: "SEQUENCE_GAP"},
		}, nil
	}

	delete(r.pendingOps, sessionID)
	ictx.Sequence = seq

	switch result.Status {
	case operationResultAccepted, operationResultExecuted:
		ictx.State = domain.InvokeStateInvoking
	case operationResultRejected, operationResultFailed:
		ictx.State = domain.InvokeStateFailed
	}

	return nil, nil
}

// extractSequence parses the sequence number from an operation ID of the form
// "op-{sessionID}-{seq}".
func extractSequence(operationID string) (int64, bool) {
	lastDash := strings.LastIndex(operationID, "-")
	if lastDash < 0 || lastDash >= len(operationID)-1 {
		return 0, false
	}
	seq, err := strconv.ParseInt(operationID[lastDash+1:], 10, 64)
	if err != nil {
		return 0, false
	}
	return seq, true
}
