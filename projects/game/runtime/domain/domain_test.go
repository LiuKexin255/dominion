package domain

import (
	"testing"
	"time"
)

func TestStreamState(t *testing.T) {
	// given
	tests := []struct {
		name  string
		state StreamState
		want  int
	}{
		{name: "unspecified is zero", state: StreamStateUnspecified, want: 0},
		{name: "active is 1", state: StreamStateActive, want: 1},
		{name: "paused is 2", state: StreamStatePaused, want: 2},
		{name: "unavailable is 3", state: StreamStateUnavailable, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := int(tt.state)

			// then
			if got != tt.want {
				t.Fatalf("int(%v) = %d, want %d", tt.state, got, tt.want)
			}
		})
	}
}

func TestSessionRuntime(t *testing.T) {
	now := time.Now().UTC()

	// given
	tests := []struct {
		name    string
		runtime SessionRuntime
		check   func(t *testing.T, rt SessionRuntime)
	}{
		{
			name:    "empty runtime has no connections",
			runtime: SessionRuntime{SessionID: "s1", RuntimeID: "rt-0"},
			check: func(t *testing.T, rt SessionRuntime) {
				if rt.AgentConn != nil {
					t.Fatalf("AgentConn = %v, want nil", rt.AgentConn)
				}
				if len(rt.WebConns) != 0 {
					t.Fatalf("len(WebConns) = %d, want 0", len(rt.WebConns))
				}
				if rt.InflightOp != nil {
					t.Fatalf("InflightOp = %v, want nil", rt.InflightOp)
				}
				if rt.LatestSnapshot != nil {
					t.Fatalf("LatestSnapshot = %v, want nil", rt.LatestSnapshot)
				}
			},
		},
		{
			name: "runtime with agent and web connections",
			runtime: SessionRuntime{
				SessionID: "s1",
				RuntimeID: "rt-0",
				AgentConn: &AgentConnection{ConnID: "agent-1"},
				WebConns: []*WebConnection{
					{ConnID: "web-1"},
					{ConnID: "web-2"},
				},
				StreamState: StreamStateActive,
			},
			check: func(t *testing.T, rt SessionRuntime) {
				if rt.AgentConn.ConnID != "agent-1" {
					t.Fatalf("AgentConn.ConnID = %q, want %q", rt.AgentConn.ConnID, "agent-1")
				}
				if len(rt.WebConns) != 2 {
					t.Fatalf("len(WebConns) = %d, want 2", len(rt.WebConns))
				}
				if rt.StreamState != StreamStateActive {
					t.Fatalf("StreamState = %v, want StreamStateActive", rt.StreamState)
				}
			},
		},
		{
			name: "runtime with inflight operation",
			runtime: SessionRuntime{
				SessionID: "s1",
				RuntimeID: "rt-0",
				InflightOp: &InflightOperation{
					OperationID:     "op-1",
					Kind:            OperationKindMouseClick,
					FlashSnapshot:   true,
					CreateTime:      now,
					RequesterConnID: "web-1",
				},
			},
			check: func(t *testing.T, rt SessionRuntime) {
				if rt.InflightOp.OperationID != "op-1" {
					t.Fatalf("InflightOp.OperationID = %q, want %q", rt.InflightOp.OperationID, "op-1")
				}
				if rt.InflightOp.Kind != OperationKindMouseClick {
					t.Fatalf("InflightOp.Kind = %q, want %q", rt.InflightOp.Kind, OperationKindMouseClick)
				}
				if !rt.InflightOp.FlashSnapshot {
					t.Fatalf("InflightOp.FlashSnapshot = false, want true")
				}
				if rt.InflightOp.RequesterConnID != "web-1" {
					t.Fatalf("InflightOp.RequesterConnID = %q, want %q", rt.InflightOp.RequesterConnID, "web-1")
				}
			},
		},
		{
			name: "runtime with snapshot and timestamps",
			runtime: SessionRuntime{
				SessionID:      "s1",
				RuntimeID:      "rt-0",
				LatestSnapshot: &SnapshotRef{Data: []byte("img"), MimeType: "image/jpeg", CaptureTime: now, Cached: true},
				LastMediaTime:  now,
			},
			check: func(t *testing.T, rt SessionRuntime) {
				if string(rt.LatestSnapshot.Data) != "img" {
					t.Fatalf("LatestSnapshot.Data = %q, want %q", string(rt.LatestSnapshot.Data), "img")
				}
				if rt.LatestSnapshot.MimeType != "image/jpeg" {
					t.Fatalf("LatestSnapshot.MimeType = %q, want %q", rt.LatestSnapshot.MimeType, "image/jpeg")
				}
				if !rt.LatestSnapshot.Cached {
					t.Fatalf("LatestSnapshot.Cached = false, want true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when / then
			tt.check(t, tt.runtime)
		})
	}
}

func TestOperationKind(t *testing.T) {
	// given
	tests := []struct {
		name string
		kind OperationKind
		want string
	}{
		{name: "mouse click", kind: OperationKindMouseClick, want: "mouse_click"},
		{name: "mouse double click", kind: OperationKindMouseDoubleClick, want: "mouse_double_click"},
		{name: "mouse drag", kind: OperationKindMouseDrag, want: "mouse_drag"},
		{name: "mouse hover", kind: OperationKindMouseHover, want: "mouse_hover"},
		{name: "mouse hold", kind: OperationKindMouseHold, want: "mouse_hold"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := string(tt.kind)

			// then
			if got != tt.want {
				t.Fatalf("string(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestTimeoutConstants(t *testing.T) {
	// given
	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{name: "click timeout is 1s", got: TimeoutClick, want: 1 * time.Second},
		{name: "drag timeout is 30s", got: TimeoutDrag, want: 30 * time.Second},
		{name: "max hold duration is 30s", got: MaxHoldDuration, want: 30 * time.Second},
		{name: "agent no response timeout is 60s", got: TimeoutAgentNoResponse, want: 60 * time.Second},
		{name: "snapshot fresh threshold is 1s", got: SnapshotFreshThreshold, want: 1 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when / then
			if tt.got != tt.want {
				t.Fatalf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestDomainErrors(t *testing.T) {
	// given
	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{name: "session not found", err: ErrSessionNotFound, wantMsg: "session not found"},
		{name: "agent already connected", err: ErrAgentAlreadyConnected, wantMsg: "agent already connected"},
		{name: "no agent", err: ErrNoAgent, wantMsg: "no agent connected"},
		{name: "operation inflight", err: ErrOperationInflight, wantMsg: "operation already inflight"},
		{name: "invalid mouse action", err: ErrInvalidMouseAction, wantMsg: "invalid mouse action"},
		{name: "hold duration exceeded", err: ErrHoldDurationExceeded, wantMsg: "hold duration exceeds maximum"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := tt.err.Error()

			// then
			if got != tt.wantMsg {
				t.Fatalf("Error() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestV2DomainErrors(t *testing.T) {
	// given
	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{name: "stream mismatch", err: ErrStreamMismatch, wantMsg: "stream mismatch"},
		{name: "unknown init ID", err: ErrUnknownInitID, wantMsg: "unknown init segment ID"},
		{name: "sequence not increasing", err: ErrSequenceNotIncreasing, wantMsg: "sequence not increasing"},
		{name: "random access missing", err: ErrRandomAccessMissing, wantMsg: "random access point missing"},
		{name: "init hash mismatch", err: ErrInitHashMismatch, wantMsg: "init segment hash mismatch"},
		{name: "unsupported codec", err: ErrUnsupportedCodec, wantMsg: "unsupported codec"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := tt.err.Error()

			// then
			if got != tt.wantMsg {
				t.Fatalf("Error() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestSegmentRef(t *testing.T) {
	now := time.Now().UTC()

	// given
	seg := &SegmentRef{
		StreamID:      "stream-1",
		InitID:        "init-1",
		Sequence:      1,
		Data:          []byte("fMP4-data"),
		RandomAccess:  true,
		DurationMS:    1000,
		Discontinuity: false,
		MediaTime:     now,
	}

	// when / then
	if seg.StreamID != "stream-1" {
		t.Fatalf("StreamID = %q, want %q", seg.StreamID, "stream-1")
	}
	if seg.InitID != "init-1" {
		t.Fatalf("InitID = %q, want %q", seg.InitID, "init-1")
	}
	if seg.Sequence != 1 {
		t.Fatalf("Sequence = %d, want %d", seg.Sequence, 1)
	}
	if string(seg.Data) != "fMP4-data" {
		t.Fatalf("Data = %q, want %q", string(seg.Data), "fMP4-data")
	}
	if !seg.RandomAccess {
		t.Fatalf("RandomAccess = false, want true")
	}
	if seg.DurationMS != 1000 {
		t.Fatalf("DurationMS = %d, want %d", seg.DurationMS, 1000)
	}
	if seg.Discontinuity {
		t.Fatalf("Discontinuity = true, want false")
	}
	if !seg.MediaTime.Equal(now) {
		t.Fatalf("MediaTime = %v, want %v", seg.MediaTime, now)
	}
}

func TestInitSegmentRef(t *testing.T) {
	// given
	ref := &InitSegmentRef{
		StreamID: "stream-1",
		InitID:   "init-1",
		Codec:    "avc1.64001f",
		MimeType: "video/mp4",
		Data:     []byte("init-segment"),
	}

	// when / then
	if ref.StreamID != "stream-1" {
		t.Fatalf("StreamID = %q, want %q", ref.StreamID, "stream-1")
	}
	if ref.InitID != "init-1" {
		t.Fatalf("InitID = %q, want %q", ref.InitID, "init-1")
	}
	if ref.Codec != "avc1.64001f" {
		t.Fatalf("Codec = %q, want %q", ref.Codec, "avc1.64001f")
	}
	if ref.MimeType != "video/mp4" {
		t.Fatalf("MimeType = %q, want %q", ref.MimeType, "video/mp4")
	}
	if string(ref.Data) != "init-segment" {
		t.Fatalf("Data = %q, want %q", string(ref.Data), "init-segment")
	}
}

func TestRouteTargetKind(t *testing.T) {
	// given
	tests := []struct {
		name string
		kind RouteTargetKind
		want int
	}{
		{name: "agent is 0", kind: RouteTargetAgent, want: 0},
		{name: "web broadcast is 1", kind: RouteTargetWebBroadcast, want: 1},
		{name: "conn is 2", kind: RouteTargetConn, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := int(tt.kind)

			// then
			if got != tt.want {
				t.Fatalf("int(%v) = %d, want %d", tt.kind, got, tt.want)
			}
		})
	}
}

func TestRouteTargetKindDistinct(t *testing.T) {
	// given
	values := map[int]string{
		int(RouteTargetAgent):        "RouteTargetAgent",
		int(RouteTargetWebBroadcast): "RouteTargetWebBroadcast",
		int(RouteTargetConn):         "RouteTargetConn",
	}

	// when / then
	if len(values) != 3 {
		t.Fatalf("expected 3 distinct values, got %d", len(values))
	}
}

func TestControlResultStatus(t *testing.T) {
	// given
	tests := []struct {
		name   string
		status ControlResultStatus
		want   int
	}{
		{name: "succeeded is 1", status: ControlResultStatusSucceeded, want: 1},
		{name: "failed is 2", status: ControlResultStatusFailed, want: 2},
		{name: "timed out is 3", status: ControlResultStatusTimedOut, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := int(tt.status)

			// then
			if got != tt.want {
				t.Fatalf("int(%v) = %d, want %d", tt.status, got, tt.want)
			}
		})
	}
}

func TestControlResultPayload(t *testing.T) {
	// given
	tests := []struct {
		name    string
		payload ControlResultPayload
		check   func(t *testing.T, p ControlResultPayload)
	}{
		{
			name: "succeeded result",
			payload: ControlResultPayload{
				OperationID: "op-1",
				Status:      ControlResultStatusSucceeded,
			},
			check: func(t *testing.T, p ControlResultPayload) {
				if p.OperationID != "op-1" {
					t.Fatalf("OperationID = %q, want %q", p.OperationID, "op-1")
				}
				if p.Status != ControlResultStatusSucceeded {
					t.Fatalf("Status = %d, want %d", p.Status, ControlResultStatusSucceeded)
				}
				if p.ErrorMessage != "" {
					t.Fatalf("ErrorMessage = %q, want empty", p.ErrorMessage)
				}
			},
		},
		{
			name: "failed result with error",
			payload: ControlResultPayload{
				OperationID:  "op-2",
				Status:       ControlResultStatusFailed,
				ErrorMessage: "agent error",
			},
			check: func(t *testing.T, p ControlResultPayload) {
				if p.Status != ControlResultStatusFailed {
					t.Fatalf("Status = %d, want %d", p.Status, ControlResultStatusFailed)
				}
				if p.ErrorMessage != "agent error" {
					t.Fatalf("ErrorMessage = %q, want %q", p.ErrorMessage, "agent error")
				}
			},
		},
		{
			name: "timed out result",
			payload: ControlResultPayload{
				OperationID: "op-3",
				Status:      ControlResultStatusTimedOut,
			},
			check: func(t *testing.T, p ControlResultPayload) {
				if p.Status != ControlResultStatusTimedOut {
					t.Fatalf("Status = %d, want %d", p.Status, ControlResultStatusTimedOut)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when / then
			tt.check(t, tt.payload)
		})
	}
}

func TestControlRequestPayload(t *testing.T) {
	// given
	tests := []struct {
		name    string
		payload ControlRequestPayload
		check   func(t *testing.T, p ControlRequestPayload)
	}{
		{
			name: "click with snapshot",
			payload: ControlRequestPayload{
				OperationID:   "op-1",
				ActionKind:    OperationKindMouseClick,
				FlashSnapshot: true,
			},
			check: func(t *testing.T, p ControlRequestPayload) {
				if p.OperationID != "op-1" {
					t.Fatalf("OperationID = %q, want %q", p.OperationID, "op-1")
				}
				if p.ActionKind != OperationKindMouseClick {
					t.Fatalf("ActionKind = %q, want %q", p.ActionKind, OperationKindMouseClick)
				}
				if !p.FlashSnapshot {
					t.Fatalf("FlashSnapshot = false, want true")
				}
			},
		},
		{
			name: "drag without snapshot",
			payload: ControlRequestPayload{
				OperationID: "op-2",
				ActionKind:  OperationKindMouseDrag,
			},
			check: func(t *testing.T, p ControlRequestPayload) {
				if p.ActionKind != OperationKindMouseDrag {
					t.Fatalf("ActionKind = %q, want %q", p.ActionKind, OperationKindMouseDrag)
				}
				if p.FlashSnapshot {
					t.Fatalf("FlashSnapshot = true, want false")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when / then
			tt.check(t, tt.payload)
		})
	}
}

func TestRoutedMessage(t *testing.T) {
	// given
	tests := []struct {
		name   string
		routed RoutedMessage
		check  func(t *testing.T, r RoutedMessage)
	}{
		{
			name: "agent route",
			routed: RoutedMessage{
				Message:    Message{SessionID: "s1", MessageID: "m1"},
				TargetKind: RouteTargetAgent,
			},
			check: func(t *testing.T, r RoutedMessage) {
				if r.TargetKind != RouteTargetAgent {
					t.Fatalf("TargetKind = %d, want %d", r.TargetKind, RouteTargetAgent)
				}
				if r.TargetConnID != "" {
					t.Fatalf("TargetConnID = %q, want empty", r.TargetConnID)
				}
				if r.Message.SessionID != "s1" {
					t.Fatalf("Message.SessionID = %q, want %q", r.Message.SessionID, "s1")
				}
			},
		},
		{
			name: "conn route with target",
			routed: RoutedMessage{
				Message:      Message{SessionID: "s2"},
				TargetKind:   RouteTargetConn,
				TargetConnID: "conn-1",
			},
			check: func(t *testing.T, r RoutedMessage) {
				if r.TargetKind != RouteTargetConn {
					t.Fatalf("TargetKind = %d, want %d", r.TargetKind, RouteTargetConn)
				}
				if r.TargetConnID != "conn-1" {
					t.Fatalf("TargetConnID = %q, want %q", r.TargetConnID, "conn-1")
				}
			},
		},
		{
			name: "web broadcast",
			routed: RoutedMessage{
				Message:    Message{SessionID: "s3"},
				TargetKind: RouteTargetWebBroadcast,
			},
			check: func(t *testing.T, r RoutedMessage) {
				if r.TargetKind != RouteTargetWebBroadcast {
					t.Fatalf("TargetKind = %d, want %d", r.TargetKind, RouteTargetWebBroadcast)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when / then
			tt.check(t, tt.routed)
		})
	}
}
