// Package invoke provides the InvokeRuntime implementation for the agent runtime.
// InvokeRuntime implements domain.Runtime using an in-memory state machine with
// deterministic mock operation output.
package invoke

import (
	"context"
	"fmt"
	"sync"

	"dominion/projects/game/agent/domain"
)

// InvokeRuntime is an in-memory implementation of domain.Runtime that provides
// a full invoke state machine with deterministic mock operation output.
// It stores agent sessions in memory and produces center-click mouse operations
// in response to every screenshot.
type InvokeRuntime struct {
	mu      sync.Mutex
	agents  map[string]*domain.InvokeContext
	counter int
}

// New creates a new InvokeRuntime. The promptClient parameter is reserved for
// future features.
func New(promptClient domain.PromptServiceClient) *InvokeRuntime {
	_ = promptClient // reserved for future features
	return &InvokeRuntime{
		agents: make(map[string]*domain.InvokeContext),
	}
}

// CreateWithProfile creates an agent session with the given runtime configuration.
// The config must contain at least a non-empty ProfileName.
func (r *InvokeRuntime) CreateWithProfile(_ context.Context, sessionID string, config *domain.InvokeRuntimeConfig) (*domain.Status, error) {
	if config.ProfileName == "" {
		return nil, fmt.Errorf("empty profile name")
	}

	skillNames := make([]string, len(config.Skills))
	for i, sk := range config.Skills {
		skillNames[i] = sk.SkillName
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.agents[sessionID] = &domain.InvokeContext{
		SessionID:   sessionID,
		State:       domain.InvokeStateIdle,
		ProfileName: config.ProfileName,
		Skills:      skillNames,
		MCPNames:    config.MCPNames,
	}

	return &domain.Status{
		SessionId: sessionID,
		Status:    "created",
	}, nil
}

// Delete removes the agent session. It is idempotent: deleting a non-existent
// session returns nil.
func (r *InvokeRuntime) Delete(_ context.Context, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, sessionID)
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
	default:
		return nil, fmt.Errorf("cannot receive screenshot in state %v", ictx.State)
	}
}

// handleIdleScreenshot starts a new invoke from the Idle state.
// It increments the sequence, produces a text frame and a deterministic
// center-click operation, then transitions to Invoking.
func (r *InvokeRuntime) handleIdleScreenshot(sessionID string, input *domain.ScreenshotInput, ictx *domain.InvokeContext) ([]*domain.Frame, error) {
	r.counter++
	ictx.InvokeID = fmt.Sprintf("invoke-%s-%d", sessionID, r.counter)

	ictx.Sequence++
	opSeq := ictx.Sequence
	opID := fmt.Sprintf("op-%s-%d", sessionID, opSeq)

	frames := []*domain.Frame{
		{
			Type:    domain.FrameTypeText,
			Content: "analyzing screenshot...",
		},
		{
			Type:         domain.FrameTypeOperation,
			OperationID:  opID,
			ScreenshotID: input.CaptureId,
			Sequence:     opSeq,
			IsMouse:      true,
			Button:       1,
			ClickType:    1,
			XPx:          input.WidthPx / 2,
			YPx:          input.HeightPx / 2,
		},
	}

	ictx.State = domain.InvokeStateInvoking
	return frames, nil
}

// handleInvokingScreenshot handles a screenshot received while already invoking.
// It increments the sequence, produces a text frame and a deterministic
// center-click operation, and stays in Invoking state.
func (r *InvokeRuntime) handleInvokingScreenshot(sessionID string, input *domain.ScreenshotInput, ictx *domain.InvokeContext) ([]*domain.Frame, error) {
	ictx.Sequence++
	opSeq := ictx.Sequence
	opID := fmt.Sprintf("op-%s-%d", sessionID, opSeq)

	frames := []*domain.Frame{
		{
			Type:    domain.FrameTypeText,
			Content: "analyzing screenshot...",
		},
		{
			Type:         domain.FrameTypeOperation,
			OperationID:  opID,
			ScreenshotID: input.CaptureId,
			Sequence:     opSeq,
			IsMouse:      true,
			Button:       1,
			ClickType:    1,
			XPx:          input.WidthPx / 2,
			YPx:          input.HeightPx / 2,
		},
	}

	ictx.State = domain.InvokeStateInvoking
	return frames, nil
}
