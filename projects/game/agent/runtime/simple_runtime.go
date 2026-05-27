// Package runtime provides implementations of the agent domain.Runtime interface.
package runtime

import (
	"context"
	"io"
	"sync"
	"time"

	game "dominion/projects/game"
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

// Connect handles the bidirectional stream for agent communication.
// It reads AgentFrames from the stream and responds:
//   - "status" frames reply with the current status string.
//   - all other frames are echoed back with type "echo".
//
// Returns nil on io.EOF (clean close) or the error from Recv/Send.
func (r *SimpleRuntime) Connect(stream domain.AgentStream) error {
	for {
		frame, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		switch frame.Type {
		case "status":
			r.mu.Lock()
			s, ok := r.data[frame.SessionId]
			r.mu.Unlock()
			statusStr := "unknown"
			if ok {
				statusStr = s.Status
			}
			resp := &game.AgentFrame{
				SessionId: frame.SessionId,
				Type:      "status",
				Payload:   []byte(statusStr),
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		default:
			resp := &game.AgentFrame{
				SessionId: frame.SessionId,
				Type:      "echo",
				Payload:   frame.Payload,
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	}
}
