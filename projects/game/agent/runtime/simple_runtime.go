// Package runtime provides implementations of the agent domain.Runtime interface.
package runtime

import (
	"context"
	"sync"
	"time"

	"dominion/projects/game/agent/domain"
)

// SimpleRuntime is an in-memory implementation of domain.Runtime.
// It stores agent session status in a map protected by a mutex.
// No external dependencies (DB, gRPC clients) are required.
type SimpleRuntime struct {
	mu   sync.Mutex
	data map[string]*domain.Status
}

// NewSimpleRuntime creates a new SimpleRuntime with an empty status map.
func NewSimpleRuntime() *SimpleRuntime {
	return &SimpleRuntime{
		data: make(map[string]*domain.Status),
	}
}

// Create creates the agent session with status "initialized" and returns a copy.
func (r *SimpleRuntime) Create(ctx context.Context, sessionID string) (*domain.Status, error) {
	s := &domain.Status{
		SessionId:  sessionID,
		Status:     "initialized",
		CreateTime: time.Now(),
	}
	r.mu.Lock()
	r.data[sessionID] = s
	r.mu.Unlock()
	cp := *s
	return &cp, nil
}

// Delete removes the agent session from the in-memory map.
// It is idempotent: deleting a non-existent session returns nil.
func (r *SimpleRuntime) Delete(_ context.Context, sessionID string) error {
	r.mu.Lock()
	delete(r.data, sessionID)
	r.mu.Unlock()
	return nil
}

// Status returns the current status for the session.
// Returns "initialized" if the session exists, otherwise "unknown".
func (r *SimpleRuntime) Status(ctx context.Context, sessionID string) (*domain.Status, error) {
	r.mu.Lock()
	s, ok := r.data[sessionID]
	r.mu.Unlock()
	if ok {
		cp := *s
		return &cp, nil
	}
	return &domain.Status{
		SessionId: sessionID,
		Status:    "unknown",
	}, nil
}

// CreateWithProfile delegates to Create since SimpleRuntime does not perform
// profile validation.
func (r *SimpleRuntime) CreateWithProfile(ctx context.Context, sessionID string, _ string) (*domain.Status, error) {
	return r.Create(ctx, sessionID)
}

// ReceiveScreenshot acknowledges a screenshot from a client and returns a
// single text frame indicating receipt.
func (r *SimpleRuntime) ReceiveScreenshot(_ context.Context, _ string, _ *domain.ScreenshotInput) ([]*domain.Frame, error) {
	return []*domain.Frame{
		{
			Type:    domain.FrameTypeText,
			Content: "screenshot received",
		},
	}, nil
}

// ReceiveOperationResult is a no-op for SimpleRuntime.
func (r *SimpleRuntime) ReceiveOperationResult(_ context.Context, _ string, _ *domain.OperationResult) ([]*domain.Frame, error) {
	return nil, nil
}
