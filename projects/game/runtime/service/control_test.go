package service

import (
	"errors"
	"testing"
	"time"

	"dominion/projects/game/runtime/domain"
)

func TestControlExecutor_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     domain.ControlRequestPayload
		wantErr error
	}{
		{
			name: "click accepted",
			req: domain.ControlRequestPayload{
				OperationID: "op-1",
				ActionKind:  domain.OperationKindMouseClick,
			},
			wantErr: nil,
		},
		{
			name: "double click accepted",
			req: domain.ControlRequestPayload{
				OperationID: "op-2",
				ActionKind:  domain.OperationKindMouseDoubleClick,
			},
			wantErr: nil,
		},
		{
			name: "hover accepted",
			req: domain.ControlRequestPayload{
				OperationID: "op-3",
				ActionKind:  domain.OperationKindMouseHover,
			},
			wantErr: nil,
		},
		{
			name: "drag accepted",
			req: domain.ControlRequestPayload{
				OperationID: "op-4",
				ActionKind:  domain.OperationKindMouseDrag,
			},
			wantErr: nil,
		},
		{
			name: "hold accepted",
			req: domain.ControlRequestPayload{
				OperationID: "op-5",
				ActionKind:  domain.OperationKindMouseHold,
			},
			wantErr: nil,
		},
		{
			name: "zero value request rejected",
			req: domain.ControlRequestPayload{
				OperationID: "op-empty",
			},
			wantErr: domain.ErrInvalidMouseAction,
		},
		{
			name: "unspecified kind rejected",
			req: domain.ControlRequestPayload{
				OperationID: "op-unspec",
				ActionKind:  domain.OperationKind(""),
			},
			wantErr: domain.ErrInvalidMouseAction,
		},
		{
			name: "unknown kind rejected",
			req: domain.ControlRequestPayload{
				OperationID: "op-unknown",
				ActionKind:  domain.OperationKind("unknown_action"),
			},
			wantErr: domain.ErrInvalidMouseAction,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewControlExecutor()

			_, err := e.SubmitOperation("session-1", tt.req, "conn-1")

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SubmitOperation() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestControlExecutor_HoldDuration(t *testing.T) {
	e := NewControlExecutor()
	req := domain.ControlRequestPayload{
		OperationID: "op-hold",
		ActionKind:  domain.OperationKindMouseHold,
	}

	_, err := e.SubmitOperation("session-1", req, "conn-1")
	if err != nil {
		t.Fatalf("SubmitOperation() error = %v", err)
	}

	op := e.GetInflightOperation("session-1")
	if op == nil {
		t.Fatal("GetInflightOperation() returned nil, expected inflight")
	}
}

func TestControlExecutor_Inflight(t *testing.T) {
	e := NewControlExecutor()
	req := domain.ControlRequestPayload{
		OperationID: "op-1",
		ActionKind:  domain.OperationKindMouseClick,
	}

	_, err := e.SubmitOperation("session-1", req, "conn-1")
	if err != nil {
		t.Fatalf("first SubmitOperation() error = %v, want nil", err)
	}

	_, err = e.SubmitOperation("session-1", req, "conn-2")
	if !errors.Is(err, domain.ErrOperationInflight) {
		t.Fatalf("concurrent SubmitOperation() error = %v, want %v", err, domain.ErrOperationInflight)
	}

	_, err = e.SubmitOperation("session-2", req, "conn-1")
	if err != nil {
		t.Fatalf("different session SubmitOperation() error = %v, want nil", err)
	}

	_, _, err = e.HandleAgentResult("session-1")
	if err != nil {
		t.Fatalf("HandleAgentResult() error = %v", err)
	}

	_, err = e.SubmitOperation("session-1", req, "conn-1")
	if err != nil {
		t.Fatalf("resubmit after result SubmitOperation() error = %v, want nil", err)
	}
}

func TestControlExecutor_Timeout(t *testing.T) {
	e := NewControlExecutor()

	req := domain.ControlRequestPayload{
		OperationID: "op-timeout",
		ActionKind:  domain.OperationKindMouseClick,
	}

	_, err := e.SubmitOperation("session-1", req, "conn-1")
	if err != nil {
		t.Fatalf("SubmitOperation() error = %v", err)
	}

	var gotCompletion domain.ControlCompletion
	select {
	case gotCompletion = <-e.Completions():
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for completion")
	}

	if gotCompletion.SessionID != "session-1" {
		t.Fatalf("SessionID = %q, want %q", gotCompletion.SessionID, "session-1")
	}
	if gotCompletion.RequesterConnID != "conn-1" {
		t.Fatalf("RequesterConnID = %q, want %q", gotCompletion.RequesterConnID, "conn-1")
	}
	if gotCompletion.Result.OperationID != "op-timeout" {
		t.Fatalf("result OperationID = %q, want %q", gotCompletion.Result.OperationID, "op-timeout")
	}
	if gotCompletion.Result.Status != domain.ControlResultStatusTimedOut {
		t.Fatalf("result Status = %d, want %d (TimedOut)", gotCompletion.Result.Status, domain.ControlResultStatusTimedOut)
	}
	if gotCompletion.Result.ErrorMessage != "timed out" {
		t.Fatalf("result ErrorMessage = %q, want %q", gotCompletion.Result.ErrorMessage, "timed out")
	}

	if e.HasInflightOperation("session-1") {
		t.Fatal("HasInflightOperation() = true after timeout, want false")
	}
}

func TestControlExecutor_FlashSnapshot(t *testing.T) {
	e := NewControlExecutor()

	req := domain.ControlRequestPayload{
		OperationID:   "op-flash",
		ActionKind:    domain.OperationKindMouseClick,
		FlashSnapshot: true,
	}

	_, err := e.SubmitOperation("session-1", req, "conn-1")
	if err != nil {
		t.Fatalf("SubmitOperation() error = %v", err)
	}

	op := e.GetInflightOperation("session-1")
	if op == nil {
		t.Fatal("GetInflightOperation() returned nil")
	}
	if !op.FlashSnapshot {
		t.Fatal("FlashSnapshot = false, want true")
	}
	if op.OperationID != "op-flash" {
		t.Fatalf("OperationID = %q, want %q", op.OperationID, "op-flash")
	}
	if op.Kind != domain.OperationKindMouseClick {
		t.Fatalf("Kind = %q, want %q", op.Kind, domain.OperationKindMouseClick)
	}
	if op.RequesterConnID != "conn-1" {
		t.Fatalf("RequesterConnID = %q, want %q", op.RequesterConnID, "conn-1")
	}

	_, _, err = e.HandleAgentResult("session-1")
	if err != nil {
		t.Fatalf("HandleAgentResult() error = %v", err)
	}
}

func TestControlExecutor_AckAndResult(t *testing.T) {
	e := NewControlExecutor()
	req := domain.ControlRequestPayload{
		OperationID: "op-ack-result",
		ActionKind:  domain.OperationKindMouseClick,
	}

	_, err := e.SubmitOperation("session-1", req, "conn-1")
	if err != nil {
		t.Fatalf("SubmitOperation() error = %v", err)
	}

	requesterConnID, err := e.HandleAgentAck("session-1")
	if err != nil {
		t.Fatalf("HandleAgentAck() error = %v", err)
	}
	if requesterConnID != "conn-1" {
		t.Fatalf("HandleAgentAck() requesterConnID = %q, want %q", requesterConnID, "conn-1")
	}

	requesterConnID, flashSnapshot, err := e.HandleAgentResult("session-1")
	if err != nil {
		t.Fatalf("HandleAgentResult() error = %v", err)
	}
	if requesterConnID != "conn-1" {
		t.Fatalf("HandleAgentResult() requesterConnID = %q, want %q", requesterConnID, "conn-1")
	}
	if flashSnapshot {
		t.Fatal("HandleAgentResult() flashSnapshot = true, want false")
	}

	if e.HasInflightOperation("session-1") {
		t.Fatal("HasInflightOperation() = true after result, want false")
	}
}

func TestControlExecutor_AgentDisconnect(t *testing.T) {
	e := NewControlExecutor()

	req := domain.ControlRequestPayload{
		OperationID: "op-disc",
		ActionKind:  domain.OperationKindMouseClick,
	}

	_, err := e.SubmitOperation("session-1", req, "conn-1")
	if err != nil {
		t.Fatalf("SubmitOperation() error = %v", err)
	}

	e.HandleAgentDisconnect("session-1")

	var gotCompletion domain.ControlCompletion
	select {
	case gotCompletion = <-e.Completions():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for completion")
	}

	if gotCompletion.SessionID != "session-1" {
		t.Fatalf("SessionID = %q, want %q", gotCompletion.SessionID, "session-1")
	}
	if gotCompletion.RequesterConnID != "conn-1" {
		t.Fatalf("RequesterConnID = %q, want %q", gotCompletion.RequesterConnID, "conn-1")
	}
	if gotCompletion.Result.OperationID != "op-disc" {
		t.Fatalf("result OperationID = %q, want %q", gotCompletion.Result.OperationID, "op-disc")
	}
	if gotCompletion.Result.Status != domain.ControlResultStatusFailed {
		t.Fatalf("result Status = %d, want %d (Failed)", gotCompletion.Result.Status, domain.ControlResultStatusFailed)
	}
	if gotCompletion.Result.ErrorMessage != "agent disconnected" {
		t.Fatalf("result ErrorMessage = %q, want %q", gotCompletion.Result.ErrorMessage, "agent disconnected")
	}

	if e.HasInflightOperation("session-1") {
		t.Fatal("HasInflightOperation() = true after disconnect, want false")
	}
}

func TestControlExecutor_HandleAgentAckNoInflight(t *testing.T) {
	e := NewControlExecutor()

	_, err := e.HandleAgentAck("session-missing")

	if !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("HandleAgentAck() error = %v, want %v", err, domain.ErrSessionNotFound)
	}
}

func TestControlExecutor_HandleAgentResultNoInflight(t *testing.T) {
	e := NewControlExecutor()

	_, _, err := e.HandleAgentResult("session-missing")

	if !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("HandleAgentResult() error = %v, want %v", err, domain.ErrSessionNotFound)
	}
}

func TestControlExecutor_HandleAgentDisconnectNoInflight(t *testing.T) {
	e := NewControlExecutor()

	e.HandleAgentDisconnect("session-missing")
}

func TestControlExecutor_HasInflightOperation(t *testing.T) {
	e := NewControlExecutor()

	if e.HasInflightOperation("session-1") {
		t.Fatal("HasInflightOperation() = true, want false")
	}

	req := domain.ControlRequestPayload{
		OperationID: "op-1",
		ActionKind:  domain.OperationKindMouseClick,
	}

	_, err := e.SubmitOperation("session-1", req, "conn-1")
	if err != nil {
		t.Fatalf("SubmitOperation() error = %v", err)
	}

	if !e.HasInflightOperation("session-1") {
		t.Fatal("HasInflightOperation() = false, want true")
	}

	_, _, err = e.HandleAgentResult("session-1")
	if err != nil {
		t.Fatalf("HandleAgentResult() error = %v", err)
	}
	if e.HasInflightOperation("session-1") {
		t.Fatal("HasInflightOperation() = true after result, want false")
	}
}

func TestControlExecutor_TimeoutDuration(t *testing.T) {
	tests := []struct {
		name        string
		req         domain.ControlRequestPayload
		wantTimeout time.Duration
	}{
		{
			name: "click timeout is 1s",
			req: domain.ControlRequestPayload{
				ActionKind: domain.OperationKindMouseClick,
			},
			wantTimeout: domain.TimeoutClick,
		},
		{
			name: "double click timeout is 1s",
			req: domain.ControlRequestPayload{
				ActionKind: domain.OperationKindMouseDoubleClick,
			},
			wantTimeout: domain.TimeoutClick,
		},
		{
			name: "hover timeout is 1s",
			req: domain.ControlRequestPayload{
				ActionKind: domain.OperationKindMouseHover,
			},
			wantTimeout: domain.TimeoutClick,
		},
		{
			name: "drag timeout is 30s",
			req: domain.ControlRequestPayload{
				ActionKind: domain.OperationKindMouseDrag,
			},
			wantTimeout: domain.TimeoutDrag,
		},
		{
			name: "hold timeout equals max hold duration",
			req: domain.ControlRequestPayload{
				ActionKind: domain.OperationKindMouseHold,
			},
			wantTimeout: domain.MaxHoldDuration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timeout, err := validateRequest(tt.req)

			if err != nil {
				t.Fatalf("validateRequest() error = %v", err)
			}
			if timeout != tt.wantTimeout {
				t.Fatalf("timeout = %v, want %v", timeout, tt.wantTimeout)
			}
		})
	}
}
