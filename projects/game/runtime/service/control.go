package service

import (
	"fmt"
	"sync"
	"time"

	"dominion/projects/game/runtime/domain"
)

const (
	logFieldSessionID = "session_id"
)

type inflightEntry struct {
	op       *domain.InflightOperation
	deadline time.Time
}

// ControlExecutor manages inflight control operations with timeouts.
type ControlExecutor struct {
	mu       sync.Mutex
	inflight map[string]*inflightEntry
	done     chan domain.ControlCompletion
}

// NewControlExecutor creates a new ControlExecutor.
func NewControlExecutor() *ControlExecutor {
	return &ControlExecutor{
		inflight: map[string]*inflightEntry{},
		done:     make(chan domain.ControlCompletion, 32),
	}
}

// Completions returns the read-only completion channel.
func (e *ControlExecutor) Completions() <-chan domain.ControlCompletion {
	return e.done
}

// SubmitOperation validates the request and registers it as an inflight
// operation. Returns the timeout duration for the operation kind, or an
// error if validation fails or an operation is already inflight for the
// session.
func (e *ControlExecutor) SubmitOperation(sessionID string, req domain.ControlRequestPayload, requesterConnID string) (time.Duration, error) {
	timeout, err := validateRequest(req)
	if err != nil {
		return 0, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.inflight[sessionID]; exists {
		return 0, domain.ErrOperationInflight
	}

	e.inflight[sessionID] = &inflightEntry{
		op: &domain.InflightOperation{
			OperationID:     req.OperationID,
			Kind:            req.ActionKind,
			FlashSnapshot:   req.FlashSnapshot,
			CreateTime:      time.Now(),
			RequesterConnID: requesterConnID,
		},
		deadline: time.Now().Add(timeout),
	}

	go e.waitTimeout(sessionID, timeout)

	return timeout, nil
}

func (e *ControlExecutor) waitTimeout(sessionID string, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	<-timer.C

	e.mu.Lock()
	entry, exists := e.inflight[sessionID]
	if !exists {
		e.mu.Unlock()
		return
	}
	delete(e.inflight, sessionID)
	e.mu.Unlock()

	entry.op.CreateTime = time.Now()

	e.done <- domain.ControlCompletion{
		SessionID:       sessionID,
		RequesterConnID: entry.op.RequesterConnID,
		Result: domain.ControlResultPayload{
			OperationID:  entry.op.OperationID,
			Status:       domain.ControlResultStatusTimedOut,
			ErrorMessage: "timed out",
		},
		FlashSnapshot: entry.op.FlashSnapshot,
	}
}

// HandleAgentAck marks the inflight operation as acknowledged and returns the
// requester connection ID.
func (e *ControlExecutor) HandleAgentAck(sessionID string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	entry, exists := e.inflight[sessionID]
	if !exists {
		return "", fmt.Errorf("%w: no inflight operation", domain.ErrSessionNotFound)
	}

	return entry.op.RequesterConnID, nil
}

// HandleAgentResult clears the inflight operation and returns the requester
// connection ID and flash_snapshot flag.
func (e *ControlExecutor) HandleAgentResult(sessionID string) (string, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	entry, exists := e.inflight[sessionID]
	if !exists {
		return "", false, fmt.Errorf("%w: no inflight operation", domain.ErrSessionNotFound)
	}

	requesterConnID := entry.op.RequesterConnID
	flashSnapshot := entry.op.FlashSnapshot

	delete(e.inflight, sessionID)

	return requesterConnID, flashSnapshot, nil
}

// HandleAgentDisconnect immediately fails any inflight operation for the
// session with "agent disconnected" error.
func (e *ControlExecutor) HandleAgentDisconnect(sessionID string) {
	e.mu.Lock()
	entry, exists := e.inflight[sessionID]
	if !exists {
		e.mu.Unlock()
		return
	}
	delete(e.inflight, sessionID)
	e.mu.Unlock()

	e.done <- domain.ControlCompletion{
		SessionID:       sessionID,
		RequesterConnID: entry.op.RequesterConnID,
		Result: domain.ControlResultPayload{
			OperationID:  entry.op.OperationID,
			Status:       domain.ControlResultStatusFailed,
			ErrorMessage: "agent disconnected",
		},
		FlashSnapshot: entry.op.FlashSnapshot,
	}
}

// HasInflightOperation returns true if the session has an inflight operation.
func (e *ControlExecutor) HasInflightOperation(sessionID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, exists := e.inflight[sessionID]
	return exists
}

// GetInflightOperation returns the inflight operation for the session, or nil.
func (e *ControlExecutor) GetInflightOperation(sessionID string) *domain.InflightOperation {
	e.mu.Lock()
	defer e.mu.Unlock()
	entry, exists := e.inflight[sessionID]
	if !exists {
		return nil
	}
	return entry.op
}

func validateRequest(req domain.ControlRequestPayload) (time.Duration, error) {
	switch req.ActionKind {
	case domain.OperationKindMouseClick,
		domain.OperationKindMouseDoubleClick,
		domain.OperationKindMouseHover:
		return domain.TimeoutClick, nil

	case domain.OperationKindMouseDrag:
		return domain.TimeoutDrag, nil

	case domain.OperationKindMouseHold:
		return domain.MaxHoldDuration, nil

	default:
		return 0, domain.ErrInvalidMouseAction
	}
}


