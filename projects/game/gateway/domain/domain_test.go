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
			runtime: SessionRuntime{SessionID: "s1", GatewayID: "gw-0"},
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
				GatewayID: "gw-0",
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
				GatewayID: "gw-0",
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
				GatewayID:      "gw-0",
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

func TestSegmentRef(t *testing.T) {
	now := time.Now().UTC()

	// given
	seg := &SegmentRef{
		SegmentID: "seg-1",
		Data:      []byte("fMP4-data"),
		KeyFrame:  true,
		MediaTime: now,
	}

	// when / then
	if seg.SegmentID != "seg-1" {
		t.Fatalf("SegmentID = %q, want %q", seg.SegmentID, "seg-1")
	}
	if string(seg.Data) != "fMP4-data" {
		t.Fatalf("Data = %q, want %q", string(seg.Data), "fMP4-data")
	}
	if !seg.KeyFrame {
		t.Fatalf("KeyFrame = false, want true")
	}
	if !seg.MediaTime.Equal(now) {
		t.Fatalf("MediaTime = %v, want %v", seg.MediaTime, now)
	}
}

func TestInitSegmentRef(t *testing.T) {
	// given
	ref := &InitSegmentRef{
		MimeType: "video/mp4",
		Data:     []byte("init-segment"),
	}

	// when / then
	if ref.MimeType != "video/mp4" {
		t.Fatalf("MimeType = %q, want %q", ref.MimeType, "video/mp4")
	}
	if string(ref.Data) != "init-segment" {
		t.Fatalf("Data = %q, want %q", string(ref.Data), "init-segment")
	}
}
