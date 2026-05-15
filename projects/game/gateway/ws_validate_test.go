package gateway

import (
	"testing"
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
					MediaSegment: &GameMediaSegment{SegmentId: "seg-1"},
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
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateControlRequest() unexpected error: %v", err)
			}
		})
	}
}
