package service

import (
	"sync"
	"time"

	"dominion/projects/game/gateway/domain"
)

type inflight struct {
	op    *domain.InflightOperation
	timer *time.Timer
}

type ControlExecutor struct {
	mu           sync.Mutex
	inflight     map[string]*inflight
	onCompletion func(domain.ControlCompletion)
}

func NewControlExecutor() *ControlExecutor {
	return &ControlExecutor{
		inflight: make(map[string]*inflight),
	}
}

func (e *ControlExecutor) SetOnCompletion(fn func(domain.ControlCompletion)) {
	e.mu.Lock()
	e.onCompletion = fn
	e.mu.Unlock()
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

	return op.op, nil
}

func (e *ControlExecutor) HandleAgentAck(sessionID string) (string, error) {
	e.mu.Lock()
	op, exists := e.inflight[sessionID]
	e.mu.Unlock()

	if !exists {
		return "", domain.ErrSessionNotFound
	}

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
	onCompletion := e.onCompletion
	e.mu.Unlock()

	if onCompletion != nil {
		onCompletion(completion)
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
	onCompletion := e.onCompletion
	e.mu.Unlock()

	if onCompletion != nil {
		onCompletion(completion)
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
