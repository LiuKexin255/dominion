package gateway

import (
	"fmt"
	"testing"

	"dominion/projects/game/gateway/domain"
)

func TestActionKindFromProto(t *testing.T) {
	tests := []struct {
		name    string
		req     *GameControlRequest
		want    domain.OperationKind
		wantErr bool
	}{
		{
			name: "mouse_click",
			req: &GameControlRequest{
				Action: &GameControlRequest_MouseClick{MouseClick: &GameMouseClick{}},
			},
			want: domain.OperationKindMouseClick,
		},
		{
			name: "mouse_double_click",
			req: &GameControlRequest{
				Action: &GameControlRequest_MouseDoubleClick{MouseDoubleClick: &GameMouseDoubleClick{}},
			},
			want: domain.OperationKindMouseDoubleClick,
		},
		{
			name: "mouse_drag",
			req: &GameControlRequest{
				Action: &GameControlRequest_MouseDrag{MouseDrag: &GameMouseDrag{}},
			},
			want: domain.OperationKindMouseDrag,
		},
		{
			name: "mouse_hover",
			req: &GameControlRequest{
				Action: &GameControlRequest_MouseHover{MouseHover: &GameMouseHover{}},
			},
			want: domain.OperationKindMouseHover,
		},
		{
			name: "mouse_hold",
			req: &GameControlRequest{
				Action: &GameControlRequest_MouseHold{MouseHold: &GameMouseHold{}},
			},
			want: domain.OperationKindMouseHold,
		},
		{
			name:    "no_action",
			req:     &GameControlRequest{},
			wantErr: true,
		},
		{
			name:    "nil_request",
			req:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ActionKindFromProto(tt.req)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ActionKindFromProto() expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ActionKindFromProto() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ActionKindFromProto() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProtoOperationKind(t *testing.T) {
	tests := []struct {
		name string
		kind domain.OperationKind
		want GameControlOperationKind
	}{
		{
			name: "mouse_click",
			kind: domain.OperationKindMouseClick,
			want: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK,
		},
		{
			name: "mouse_double_click",
			kind: domain.OperationKindMouseDoubleClick,
			want: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DOUBLE_CLICK,
		},
		{
			name: "mouse_drag",
			kind: domain.OperationKindMouseDrag,
			want: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DRAG,
		},
		{
			name: "mouse_hover",
			kind: domain.OperationKindMouseHover,
			want: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOVER,
		},
		{
			name: "mouse_hold",
			kind: domain.OperationKindMouseHold,
			want: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOLD,
		},
		{
			name: "unknown_kind",
			kind: domain.OperationKind("unknown"),
			want: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProtoOperationKind(tt.kind)
			if got != tt.want {
				t.Fatalf("ProtoOperationKind(%q) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}

func TestDomainOperationKind(t *testing.T) {
	tests := []struct {
		name    string
		kind    GameControlOperationKind
		want    domain.OperationKind
		wantErr bool
	}{
		{
			name: "mouse_click",
			kind: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK,
			want: domain.OperationKindMouseClick,
		},
		{
			name: "mouse_double_click",
			kind: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DOUBLE_CLICK,
			want: domain.OperationKindMouseDoubleClick,
		},
		{
			name: "mouse_drag",
			kind: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_DRAG,
			want: domain.OperationKindMouseDrag,
		},
		{
			name: "mouse_hover",
			kind: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOVER,
			want: domain.OperationKindMouseHover,
		},
		{
			name: "mouse_hold",
			kind: GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_MOUSE_HOLD,
			want: domain.OperationKindMouseHold,
		},
		{
			name:    "unspecified",
			kind:    GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_UNSPECIFIED,
			wantErr: true,
		},
		{
			name:    "unknown_enum_value",
			kind:    GameControlOperationKind(999),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DomainOperationKind(tt.kind)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("DomainOperationKind(%v) expected error, got %q", tt.kind, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("DomainOperationKind(%v) unexpected error: %v", tt.kind, err)
			}
			if got != tt.want {
				t.Fatalf("DomainOperationKind(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestMouseButtonConvert(t *testing.T) {
	t.Run("ProtoMouseButton", func(t *testing.T) {
		tests := []struct {
			name   string
			button string
			want   GameMouseButton
		}{
			{name: "left", button: "left", want: GameMouseButton_GAME_MOUSE_BUTTON_LEFT},
			{name: "right", button: "right", want: GameMouseButton_GAME_MOUSE_BUTTON_RIGHT},
			{name: "middle", button: "middle", want: GameMouseButton_GAME_MOUSE_BUTTON_MIDDLE},
			{name: "left_case_insensitive", button: "LEFT", want: GameMouseButton_GAME_MOUSE_BUTTON_LEFT},
			{name: "unknown_string", button: "unknown", want: GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED},
			{name: "empty_string", button: "", want: GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := ProtoMouseButton(tt.button)
				if got != tt.want {
					t.Fatalf("ProtoMouseButton(%q) = %v, want %v", tt.button, got, tt.want)
				}
			})
		}
	})

	t.Run("DomainMouseButton", func(t *testing.T) {
		tests := []struct {
			name    string
			button  GameMouseButton
			want    string
			wantErr bool
		}{
			{
				name:   "left",
				button: GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
				want:   "left",
			},
			{
				name:   "right",
				button: GameMouseButton_GAME_MOUSE_BUTTON_RIGHT,
				want:   "right",
			},
			{
				name:   "middle",
				button: GameMouseButton_GAME_MOUSE_BUTTON_MIDDLE,
				want:   "middle",
			},
			{
				name:    "unspecified",
				button:  GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED,
				wantErr: true,
			},
			{
				name:    "unknown_enum_value",
				button:  GameMouseButton(999),
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := DomainMouseButton(tt.button)

				if tt.wantErr {
					if err == nil {
						t.Fatalf("DomainMouseButton(%v) expected error, got %q", tt.button, got)
					}
					return
				}
				if err != nil {
					t.Fatalf("DomainMouseButton(%v) unexpected error: %v", tt.button, err)
				}
				if got != tt.want {
					t.Fatalf("DomainMouseButton(%v) = %q, want %q", tt.button, got, tt.want)
				}
			})
		}
	})
}

func TestMouseButtonRoundTrip(t *testing.T) {
	buttons := []string{"left", "right", "middle"}

	for _, btn := range buttons {
		t.Run(btn, func(t *testing.T) {
			proto := ProtoMouseButton(btn)
			got, err := DomainMouseButton(proto)
			if err != nil {
				t.Fatalf("round-trip DomainMouseButton(%v) unexpected error: %v", proto, err)
			}
			if got != btn {
				t.Fatalf("round-trip %q: ProtoMouseButton → DomainMouseButton = %q, want %q", btn, got, btn)
			}
		})
	}
}

// Ensure error messages contain the expected detail for debugging.
func TestMouseButtonErrorMessages(t *testing.T) {
	t.Run("DomainMouseButton_unspecified_error", func(t *testing.T) {
		_, err := DomainMouseButton(GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED)
		if err == nil {
			t.Fatal("expected error for UNSPECIFIED")
		}
		if _, ok := err.(fmt.Stringer); ok {
			t.Logf("error: %v", err)
		}
	})

	t.Run("DomainOperationKind_unspecified_error", func(t *testing.T) {
		_, err := DomainOperationKind(GameControlOperationKind_GAME_CONTROL_OPERATION_KIND_UNSPECIFIED)
		if err == nil {
			t.Fatal("expected error for UNSPECIFIED")
		}
	})
}
