package service

import (
	"context"
	"sync"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/projects/game/gateway/domain"
)

const (
	logFieldOperationID   = "operation_id"
	logFieldOperationKind = "operation_kind"
)

type inflight struct {
	op    *domain.InflightOperation
	timer *time.Timer
}

type ControlExecutor struct {
	mu           sync.Mutex
	inflight     map[string]*inflight
	completionCh chan domain.ControlCompletion
}

func NewControlExecutor() *ControlExecutor {
	return &ControlExecutor{
		inflight:     make(map[string]*inflight),
		completionCh: make(chan domain.ControlCompletion, 64),
	}
}

// Completions returns a read-only channel that receives completion events when
// control operations finish asynchronously (timeout or agent disconnect).
func (e *ControlExecutor) Completions() <-chan domain.ControlCompletion {
	return e.completionCh
}

func (e *ControlExecutor) SubmitOperation(
	sessionID string,
	req domain.ControlRequestPayload,
	requesterConnID string,
) (*domain.InflightOperation, error) {
	timeout, err := validateRequest(req)
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.inflight[sessionID]; exists {
		return nil, domain.ErrOperationInflight
	}

	op := &inflight{
		op: &domain.InflightOperation{
			OperationID:     req.RequestID,
			Kind:            req.Kind,
			FlashSnapshot:   req.FlashSnapshot,
			CreateTime:      time.Now(),
			RequesterConnID: requesterConnID,
		},
	}

	op.timer = time.AfterFunc(timeout, func() {
		e.sendTimeout(sessionID, req.RequestID)
	})

	e.inflight[sessionID] = op

	logs.Info(context.Background(), "control: operation submitted",
		event.String(logFieldSessionID, sessionID),
		event.String(logFieldOperationID, req.RequestID),
		event.String(logFieldOperationKind, string(req.Kind)),
	)

	return op.op, nil
}

func (e *ControlExecutor) HandleAgentAck(sessionID string) (string, error) {
	e.mu.Lock()
	op, exists := e.inflight[sessionID]
	e.mu.Unlock()

	if !exists {
		return "", domain.ErrSessionNotFound
	}

	logs.Info(context.Background(), "control: agent ack",
		event.String(logFieldSessionID, sessionID),
	)

	return op.op.RequesterConnID, nil
}

func (e *ControlExecutor) HandleAgentResult(sessionID string) (string, bool, error) {
	e.mu.Lock()
	op, exists := e.inflight[sessionID]
	if !exists {
		e.mu.Unlock()
		return "", false, domain.ErrSessionNotFound
	}
	delete(e.inflight, sessionID)
	op.timer.Stop()
	requesterConnID := op.op.RequesterConnID
	flashSnapshot := op.op.FlashSnapshot
	e.mu.Unlock()

	logs.Info(context.Background(), "control: agent result",
		event.String(logFieldSessionID, sessionID),
	)

	return requesterConnID, flashSnapshot, nil
}

func (e *ControlExecutor) HandleAgentDisconnect(sessionID string) {
	e.mu.Lock()
	op, exists := e.inflight[sessionID]
	if !exists {
		e.mu.Unlock()
		return
	}
	delete(e.inflight, sessionID)
	op.timer.Stop()
	completion := domain.ControlCompletion{
		SessionID:       sessionID,
		RequesterConnID: op.op.RequesterConnID,
		Result: domain.ControlResultPayload{
			RequestID: op.op.OperationID,
			Success:   false,
			Error:     "agent disconnected",
		},
		FlashSnapshot: op.op.FlashSnapshot,
	}
	onCompletion := e.completionCh
	e.mu.Unlock()

	logs.Info(context.Background(), "control: agent disconnected",
		event.String(logFieldSessionID, sessionID),
	)

	select {
	case onCompletion <- completion:
	default:
	}
}

func (e *ControlExecutor) sendTimeout(sessionID, operationID string) {
	e.mu.Lock()
	op, exists := e.inflight[sessionID]
	if !exists {
		e.mu.Unlock()
		return
	}
	delete(e.inflight, sessionID)
	completion := domain.ControlCompletion{
		SessionID:       sessionID,
		RequesterConnID: op.op.RequesterConnID,
		Result: domain.ControlResultPayload{
			RequestID: operationID,
			Success:   false,
			Error:     "timed out",
			TimedOut:  true,
		},
		FlashSnapshot: op.op.FlashSnapshot,
	}
	onCompletion := e.completionCh
	e.mu.Unlock()

	logs.Error(context.Background(), "control: operation timed out",
		event.String(logFieldSessionID, sessionID),
		event.String(logFieldOperationID, operationID),
	)

	select {
	case onCompletion <- completion:
	default:
	}
}

func validateRequest(req domain.ControlRequestPayload) (time.Duration, error) {
	switch req.Kind {
	case domain.OperationKindMouseClick,
		domain.OperationKindMouseDoubleClick,
		domain.OperationKindMouseHover:
		return domain.TimeoutClick, nil

	case domain.OperationKindMouseDrag:
		return domain.TimeoutDrag, nil

	case domain.OperationKindMouseHold:
		durationMs := req.DurationMs
		if durationMs <= 0 {
			return 0, domain.ErrInvalidMouseAction
		}
		duration := time.Duration(durationMs) * time.Millisecond
		if duration > domain.MaxHoldDuration {
			return 0, domain.ErrHoldDurationExceeded
		}
		return duration, nil

	default:
		return 0, domain.ErrInvalidMouseAction
	}
}

func (e *ControlExecutor) HasInflightOperation(sessionID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, exists := e.inflight[sessionID]
	return exists
}

func (e *ControlExecutor) GetInflightOperation(sessionID string) *domain.InflightOperation {
	e.mu.Lock()
	defer e.mu.Unlock()
	op, exists := e.inflight[sessionID]
	if !exists {
		return nil
	}
	return op.op
}
