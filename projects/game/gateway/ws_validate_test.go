package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"dominion/projects/game/gateway/domain"
)

func TestValidateWebSocketEnvelope(t *testing.T) {
	tests := []struct {
		name    string
		env     *GameWebSocketEnvelope
		wantErr bool
	}{
		{
			name: "empty session_id",
			env: &GameWebSocketEnvelope{
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Ping{Ping: &GamePing{}},
			},
			wantErr: true,
		},
		{
			name: "empty message_id",
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				Payload:   &GameWebSocketEnvelope_Ping{Ping: &GamePing{}},
			},
			wantErr: true,
		},
		{
			name: "missing payload oneof",
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
			},
			wantErr: true,
		},
		{
			name: "valid envelope",
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Ping{Ping: &GamePing{}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWebSocketEnvelope(tt.env)
			if tt.wantErr && err == nil {
				t.Fatal("ValidateWebSocketEnvelope() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateWebSocketEnvelope() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateHello(t *testing.T) {
	tests := []struct {
		name    string
		env     *GameWebSocketEnvelope
		wantErr bool
	}{
		{
			name: "missing hello payload",
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Ping{Ping: &GamePing{}},
			},
			wantErr: true,
		},
		{
			name: "valid hello with unspecified role",
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_Hello{
					Hello: &GameHello{Role: GameClientRole_GAME_CLIENT_ROLE_UNSPECIFIED},
				},
			},
			wantErr: true,
		},
		{
			name: "valid hello with agent role",
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_Hello{
					Hello: &GameHello{Role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT},
				},
			},
		},
		{
			name: "valid hello with web role",
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_Hello{
					Hello: &GameHello{Role: GameClientRole_GAME_CLIENT_ROLE_WEB},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHello(tt.env)
			if tt.wantErr && err == nil {
				t.Fatal("ValidateHello() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateHello() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateRolePayload(t *testing.T) {
	tests := []struct {
		name    string
		role    GameClientRole
		env     *GameWebSocketEnvelope
		wantErr bool
	}{
		{
			name: "agent sending media_init",
			role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_MediaInit{
					MediaInit: &GameMediaInit{MimeType: "video/mp4", Segment: []byte("init")},
				},
			},
		},
		{
			name: "agent sending control_ack",
			role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_ControlAck{
					ControlAck: &GameControlAck{OperationId: "op-1"},
				},
			},
		},
		{
			name: "agent sending control_result",
			role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_ControlResult{
					ControlResult: &GameControlResult{OperationId: "op-1"},
				},
			},
		},
		{
			name: "agent sending pong",
			role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Pong{Pong: &GamePong{Nonce: "n1"}},
			},
		},
		{
			name: "agent sending error",
			role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Error{Error: &GameError{Code: "err", Message: "test"}},
			},
		},
		{
			name: "agent sending control_request",
			role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_ControlRequest{
					ControlRequest: &GameControlRequest{OperationId: "op-1"},
				},
			},
			wantErr: true,
		},
		{
			name: "agent sending ping",
			role: GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Ping{Ping: &GamePing{Nonce: "n1"}},
			},
			wantErr: true,
		},
		{
			name: "web sending control_request",
			role: GameClientRole_GAME_CLIENT_ROLE_WEB,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_ControlRequest{
					ControlRequest: &GameControlRequest{OperationId: "op-1"},
				},
			},
		},
		{
			name: "web sending ping",
			role: GameClientRole_GAME_CLIENT_ROLE_WEB,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Ping{Ping: &GamePing{Nonce: "n1"}},
			},
		},
		{
			name: "web sending media_segment",
			role: GameClientRole_GAME_CLIENT_ROLE_WEB,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_MediaSegment{
					MediaSegment: &GameMediaSegment{StreamId: "stream-1", InitId: "init-1", Sequence: 1, Segment: []byte("seg")},
				},
			},
			wantErr: true,
		},
		{
			name: "web sending media_init",
			role: GameClientRole_GAME_CLIENT_ROLE_WEB,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_MediaInit{
					MediaInit: &GameMediaInit{MimeType: "video/mp4", Segment: []byte("init")},
				},
			},
			wantErr: true,
		},
		{
			name: "web sending control_ack",
			role: GameClientRole_GAME_CLIENT_ROLE_WEB,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload: &GameWebSocketEnvelope_ControlAck{
					ControlAck: &GameControlAck{OperationId: "op-1"},
				},
			},
			wantErr: true,
		},
		{
			name: "unspecified role",
			role: GameClientRole_GAME_CLIENT_ROLE_UNSPECIFIED,
			env: &GameWebSocketEnvelope{
				SessionId: "session-1",
				MessageId: "msg-1",
				Payload:   &GameWebSocketEnvelope_Ping{Ping: &GamePing{Nonce: "n1"}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRolePayload(tt.role, tt.env)
			if tt.wantErr && err == nil {
				t.Fatal("ValidateRolePayload() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateRolePayload() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateControlRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *GameControlRequest
		wantErr bool
	}{
		{
			name: "empty operation_id",
			req: &GameControlRequest{
				Action: &GameControlRequest_MouseClick{
					MouseClick: &GameMouseClick{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      100,
						Y:      200,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "action not set",
			req: &GameControlRequest{
				OperationId: "op-1",
			},
			wantErr: true,
		},
		{
			name: "click with valid data",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseClick{
					MouseClick: &GameMouseClick{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      100,
						Y:      200,
					},
				},
			},
		},
		{
			name: "click with unspecified button",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseClick{
					MouseClick: &GameMouseClick{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED,
						X:      100,
						Y:      200,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "click with negative x",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseClick{
					MouseClick: &GameMouseClick{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:      -1,
						Y:      200,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "double_click with valid data",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseDoubleClick{
					MouseDoubleClick: &GameMouseDoubleClick{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_RIGHT,
						X:      300,
						Y:      400,
					},
				},
			},
		},
		{
			name: "double_click with unspecified button",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseDoubleClick{
					MouseDoubleClick: &GameMouseDoubleClick{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED,
						X:      300,
						Y:      400,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "drag with valid data",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseDrag{
					MouseDrag: &GameMouseDrag{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						FromX:  10,
						FromY:  20,
						ToX:    100,
						ToY:    200,
					},
				},
			},
		},
		{
			name: "drag with unspecified button",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseDrag{
					MouseDrag: &GameMouseDrag{
						Button: GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED,
						FromX:  10,
						FromY:  20,
						ToX:    100,
						ToY:    200,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "hover with valid data",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseHover{
					MouseHover: &GameMouseHover{
						X: 500,
						Y: 600,
					},
				},
			},
		},
		{
			name: "hover with negative x",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseHover{
					MouseHover: &GameMouseHover{
						X: -1,
						Y: 600,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "hold with valid data",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseHold{
					MouseHold: &GameMouseHold{
						Button:     GameMouseButton_GAME_MOUSE_BUTTON_MIDDLE,
						X:          100,
						Y:          200,
						DurationMs: 1000,
					},
				},
			},
		},
		{
			name: "hold with unspecified button",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseHold{
					MouseHold: &GameMouseHold{
						Button:     GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED,
						X:          100,
						Y:          200,
						DurationMs: 1000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "hold with duration_ms=0",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseHold{
					MouseHold: &GameMouseHold{
						Button:     GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:          100,
						Y:          200,
						DurationMs: 0,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "hold with duration_ms=30001",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseHold{
					MouseHold: &GameMouseHold{
						Button:     GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:          100,
						Y:          200,
						DurationMs: 30001,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "hold with negative duration_ms",
			req: &GameControlRequest{
				OperationId: "op-1",
				Action: &GameControlRequest_MouseHold{
					MouseHold: &GameMouseHold{
						Button:     GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
						X:          100,
						Y:          200,
						DurationMs: -1,
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateControlRequest(tt.req)
			if tt.wantErr && err == nil {
				t.Fatal("ValidateControlRequest() expected error, got nil")
			}
		})
	}
}

var validTestSegment = []byte("ftypisommoov-test-data-for-init")

func validTestInitID() string {
	hash := sha256.Sum256(validTestSegment)
	return hex.EncodeToString(hash[:])
}

func TestValidateMediaInit(t *testing.T) {
	validInitID := validTestInitID()

	// given
	tests := []struct {
		name    string
		msg     *GameMediaInit
		wantErr bool
	}{
		{
			name: "valid init passes",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   validInitID,
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "h264-avc",
				Segment:  validTestSegment,
			},
			wantErr: false,
		},
		{
			name: "empty stream_id rejected",
			msg: &GameMediaInit{
				StreamId: "",
				InitId:   validInitID,
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "h264-avc",
				Segment:  validTestSegment,
			},
			wantErr: true,
		},
		{
			name: "empty init_id rejected",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   "",
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "h264-avc",
				Segment:  validTestSegment,
			},
			wantErr: true,
		},
		{
			name: "unsupported codec rejected",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   validInitID,
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "vp9",
				Segment:  validTestSegment,
			},
			wantErr: true,
		},
		{
			name: "invalid mime_type missing video/mp4 rejected",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   validInitID,
				MimeType: "video/webm; codecs=\"avc1\"",
				Codec:    "h264-avc",
				Segment:  validTestSegment,
			},
			wantErr: true,
		},
		{
			name: "invalid mime_type missing avc1 rejected",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   validInitID,
				MimeType: "video/mp4; codecs=\"vp9\"",
				Codec:    "h264-avc",
				Segment:  validTestSegment,
			},
			wantErr: true,
		},
		{
			name: "empty segment rejected",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   validInitID,
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "h264-avc",
				Segment:  []byte{},
			},
			wantErr: true,
		},
		{
			name: "oversized segment rejected",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   validInitID,
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "h264-avc",
				Segment:  make([]byte, domain.MaxSegmentSize+1),
			},
			wantErr: true,
		},
		{
			name: "init_id hash mismatch rejected",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   "wrong-hash-value",
				MimeType: "video/mp4; codecs=\"avc1.64001f\"",
				Codec:    "h264-avc",
				Segment:  validTestSegment,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := ValidateMediaInit(tt.msg)

			// then
			if tt.wantErr && err == nil {
				t.Fatal("ValidateMediaInit() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateMediaInit() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateMediaSegment(t *testing.T) {
	// given
	ra := true

	tests := []struct {
		name    string
		msg     *GameMediaSegment
		wantErr bool
	}{
		{
			name: "valid segment passes",
			msg: &GameMediaSegment{
				StreamId:     "stream-1",
				InitId:       "init-1",
				Sequence:     1,
				Segment:      []byte("moof-mdat-data"),
				RandomAccess: &ra,
			},
			wantErr: false,
		},
		{
			name: "empty stream_id rejected",
			msg: &GameMediaSegment{
				StreamId:     "",
				InitId:       "init-1",
				Sequence:     1,
				Segment:      []byte("moof-mdat-data"),
				RandomAccess: &ra,
			},
			wantErr: true,
		},
		{
			name: "empty init_id rejected",
			msg: &GameMediaSegment{
				StreamId:     "stream-1",
				InitId:       "",
				Sequence:     1,
				Segment:      []byte("moof-mdat-data"),
				RandomAccess: &ra,
			},
			wantErr: true,
		},
		{
			name: "sequence zero rejected",
			msg: &GameMediaSegment{
				StreamId:     "stream-1",
				InitId:       "init-1",
				Sequence:     0,
				Segment:      []byte("moof-mdat-data"),
				RandomAccess: &ra,
			},
			wantErr: true,
		},
		{
			name: "empty segment rejected",
			msg: &GameMediaSegment{
				StreamId:     "stream-1",
				InitId:       "init-1",
				Sequence:     1,
				Segment:      []byte{},
				RandomAccess: &ra,
			},
			wantErr: true,
		},
		{
			name: "oversized segment rejected",
			msg: &GameMediaSegment{
				StreamId:     "stream-1",
				InitId:       "init-1",
				Sequence:     1,
				Segment:      make([]byte, domain.MaxSegmentSize+1),
				RandomAccess: &ra,
			},
			wantErr: true,
		},
		{
			name: "random_access nil rejected",
			msg: &GameMediaSegment{
				StreamId: "stream-1",
				InitId:   "init-1",
				Sequence: 1,
				Segment:  []byte("moof-mdat-data"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := ValidateMediaSegment(tt.msg)

			// then
			if tt.wantErr && err == nil {
				t.Fatal("ValidateMediaSegment() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateMediaSegment() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateMediaInit_ErrorMessages(t *testing.T) {
	validInitID := validTestInitID()

	// given
	tests := []struct {
		name       string
		msg        *GameMediaInit
		wantSubstr string
	}{
		{
			name: "unsupported codec mentions codec",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   validInitID,
				MimeType: "video/mp4; codecs=\"avc1\"",
				Codec:    "vp9",
				Segment:  validTestSegment,
			},
			wantSubstr: "unsupported codec",
		},
		{
			name: "init_id mismatch mentions mismatch",
			msg: &GameMediaInit{
				StreamId: "stream-1",
				InitId:   "bad-hash",
				MimeType: "video/mp4; codecs=\"avc1\"",
				Codec:    "h264-avc",
				Segment:  validTestSegment,
			},
			wantSubstr: "mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := ValidateMediaInit(tt.msg)

			// then
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantSubstr)
			}
		})
	}
}
