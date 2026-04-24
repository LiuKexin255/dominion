package service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"dominion/projects/game/gateway/domain"
)

func TestControlExecutor_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     domain.ControlRequestPayload
		wantErr error
	}{
		{
			name: "click with coordinates accepted",
			req: domain.ControlRequestPayload{
				RequestID: "op-1",
				Kind:      domain.OperationKindMouseClick,
				X:         100,
				Y:         200,
			},
			wantErr: nil,
		},
		{
			name: "double click with coordinates accepted",
			req: domain.ControlRequestPayload{
				RequestID: "op-2",
				Kind:      domain.OperationKindMouseDoubleClick,
				X:         50,
				Y:         75,
			},
			wantErr: nil,
		},
		{
			name: "hover with coordinates accepted",
			req: domain.ControlRequestPayload{
				RequestID: "op-3",
				Kind:      domain.OperationKindMouseHover,
				X:         0,
				Y:         0,
			},
			wantErr: nil,
		},
		{
			name: "drag with all coordinates accepted",
			req: domain.ControlRequestPayload{
				RequestID: "op-4",
				Kind:      domain.OperationKindMouseDrag,
				X:         10,
				Y:         20,
			},
			wantErr: nil,
		},
		{
			name: "hold with valid duration accepted",
			req: domain.ControlRequestPayload{
				RequestID:  "op-5",
				Kind:       domain.OperationKindMouseHold,
				X:          100,
				Y:          200,
				DurationMs: 5000,
			},
			wantErr: nil,
		},
		{
			name: "zero value request rejected",
			req: domain.ControlRequestPayload{
				RequestID: "op-empty",
			},
			wantErr: domain.ErrInvalidMouseAction,
		},
		{
			name: "unspecified kind rejected",
			req: domain.ControlRequestPayload{
				RequestID: "op-unspec",
				Kind:      domain.OperationKind(""),
				X:         0,
				Y:         0,
			},
			wantErr: domain.ErrInvalidMouseAction,
		},
		{
			name: "hold with zero duration rejected",
			req: domain.ControlRequestPayload{
				RequestID:  "op-zero",
				Kind:       domain.OperationKindMouseHold,
				X:          100,
				Y:          200,
				DurationMs: 0,
			},
			wantErr: domain.ErrInvalidMouseAction,
		},
		{
			name: "hold with negative duration rejected",
			req: domain.ControlRequestPayload{
				RequestID:  "op-neg",
				Kind:       domain.OperationKindMouseHold,
				X:          100,
				Y:          200,
				DurationMs: -1,
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
	tests := []struct {
		name        string
		durationMs  int32
		wantErr     error
		wantTimeout time.Duration
	}{
		{
			name:        "30000ms accepted",
			durationMs:  30000,
			wantErr:     nil,
			wantTimeout: 30000 * time.Millisecond,
		},
		{
			name:       "30001ms rejected",
			durationMs: 30001,
			wantErr:    domain.ErrHoldDurationExceeded,
		},
		{
			name:        "1ms accepted",
			durationMs:  1,
			wantErr:     nil,
			wantTimeout: 1 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewControlExecutor()
			req := domain.ControlRequestPayload{
				RequestID:  "op-hold",
				Kind:       domain.OperationKindMouseHold,
				X:          100,
				Y:          200,
				DurationMs: tt.durationMs,
			}

			_, err := e.SubmitOperation("session-1", req, "conn-1")

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SubmitOperation() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}

			op := e.GetInflightOperation("session-1")
			if op == nil {
				t.Fatal("GetInflightOperation() returned nil, expected inflight")
			}
		})
	}
}

func TestControlExecutor_Inflight(t *testing.T) {
	e := NewControlExecutor()
	req := domain.ControlRequestPayload{
		RequestID: "op-1",
		Kind:      domain.OperationKindMouseClick,
		X:         100,
		Y:         200,
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
	var mu sync.Mutex
	var gotCompletion domain.ControlCompletion

	e := NewControlExecutor()
	e.SetOnCompletion(func(comp domain.ControlCompletion) {
		mu.Lock()
		gotCompletion = comp
		mu.Unlock()
	})

	req := domain.ControlRequestPayload{
		RequestID: "op-timeout",
		Kind:      domain.OperationKindMouseClick,
		X:         100,
		Y:         200,
	}

	_, err := e.SubmitOperation("session-1", req, "conn-1")
	if err != nil {
		t.Fatalf("SubmitOperation() error = %v", err)
	}

	// wait for timeout (click timeout = 1s)
	deadline := time.After(3 * time.Second)
	for {
		mu.Lock()
		done := gotCompletion.Result.RequestID != ""
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for completion callback")
		case <-time.After(50 * time.Millisecond):
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if gotCompletion.SessionID != "session-1" {
		t.Fatalf("SessionID = %q, want %q", gotCompletion.SessionID, "session-1")
	}
	if gotCompletion.RequesterConnID != "conn-1" {
		t.Fatalf("RequesterConnID = %q, want %q", gotCompletion.RequesterConnID, "conn-1")
	}
	if gotCompletion.Result.RequestID != "op-timeout" {
		t.Fatalf("result RequestID = %q, want %q", gotCompletion.Result.RequestID, "op-timeout")
	}
	if gotCompletion.Result.Success {
		t.Fatal("result.Success = true, want false for timeout")
	}
	if gotCompletion.Result.Error != "timed out" {
		t.Fatalf("result.Error = %q, want %q", gotCompletion.Result.Error, "timed out")
	}
	if !gotCompletion.Result.TimedOut {
		t.Fatal("result.TimedOut = false, want true")
	}

	if e.HasInflightOperation("session-1") {
		t.Fatal("HasInflightOperation() = true after timeout, want false")
	}
}

func TestControlExecutor_FlashSnapshot(t *testing.T) {
	e := NewControlExecutor()

	req := domain.ControlRequestPayload{
		RequestID:     "op-flash",
		Kind:          domain.OperationKindMouseClick,
		FlashSnapshot: true,
		X:             100,
		Y:             200,
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
		RequestID: "op-ack-result",
		Kind:      domain.OperationKindMouseClick,
		X:         50,
		Y:         75,
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
	var mu sync.Mutex
	var gotCompletion domain.ControlCompletion

	e := NewControlExecutor()
	e.SetOnCompletion(func(comp domain.ControlCompletion) {
		mu.Lock()
		gotCompletion = comp
		mu.Unlock()
	})

	req := domain.ControlRequestPayload{
		RequestID: "op-disc",
		Kind:      domain.OperationKindMouseClick,
		X:         100,
		Y:         200,
	}

	_, err := e.SubmitOperation("session-1", req, "conn-1")
	if err != nil {
		t.Fatalf("SubmitOperation() error = %v", err)
	}

	e.HandleAgentDisconnect("session-1")

	mu.Lock()
	if gotCompletion.SessionID != "session-1" {
		t.Fatalf("SessionID = %q, want %q", gotCompletion.SessionID, "session-1")
	}
	if gotCompletion.RequesterConnID != "conn-1" {
		t.Fatalf("RequesterConnID = %q, want %q", gotCompletion.RequesterConnID, "conn-1")
	}
	if gotCompletion.Result.RequestID != "op-disc" {
		t.Fatalf("result RequestID = %q, want %q", gotCompletion.Result.RequestID, "op-disc")
	}
	if gotCompletion.Result.Success {
		t.Fatal("result.Success = true, want false for disconnect")
	}
	if gotCompletion.Result.Error != "agent disconnected" {
		t.Fatalf("result.Error = %q, want %q", gotCompletion.Result.Error, "agent disconnected")
	}
	mu.Unlock()

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
		RequestID: "op-1",
		Kind:      domain.OperationKindMouseClick,
		X:         0,
		Y:         0,
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
				Kind: domain.OperationKindMouseClick,
				X:    0,
				Y:    0,
			},
			wantTimeout: domain.TimeoutClick,
		},
		{
			name: "double click timeout is 1s",
			req: domain.ControlRequestPayload{
				Kind: domain.OperationKindMouseDoubleClick,
				X:    0,
				Y:    0,
			},
			wantTimeout: domain.TimeoutClick,
		},
		{
			name: "hover timeout is 1s",
			req: domain.ControlRequestPayload{
				Kind: domain.OperationKindMouseHover,
				X:    0,
				Y:    0,
			},
			wantTimeout: domain.TimeoutClick,
		},
		{
			name: "drag timeout is 30s",
			req: domain.ControlRequestPayload{
				Kind: domain.OperationKindMouseDrag,
				X:    0,
				Y:    0,
			},
			wantTimeout: domain.TimeoutDrag,
		},
		{
			name: "hold timeout equals duration",
			req: domain.ControlRequestPayload{
				Kind:       domain.OperationKindMouseHold,
				X:          0,
				Y:          0,
				DurationMs: 5000,
			},
			wantTimeout: 5 * time.Second,
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
