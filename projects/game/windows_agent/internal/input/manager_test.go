package input

import (
	"encoding/json"
	"testing"
	"time"

	gw "dominion/projects/game/gateway/domain"
)

func TestConvertControlRequest(t *testing.T) {
	hwnd := uintptr(12345)

	tests := []struct {
		name    string
		payload *gw.ControlRequestPayload
		want    Command
		wantErr bool
	}{
		{
			name: "mouse click",
			payload: &gw.ControlRequestPayload{
				Kind:   gw.OperationKindMouseClick,
				Button: "left",
				X:      100,
				Y:      200,
			},
			want: Command{
				Action: ActionMouseClick,
				Button: ButtonLeft,
				X:      100,
				Y:      200,
				HWND:   hwnd,
			},
		},
		{
			name: "mouse double click",
			payload: &gw.ControlRequestPayload{
				Kind:   gw.OperationKindMouseDoubleClick,
				Button: "left",
				X:      150,
				Y:      250,
			},
			want: Command{
				Action: ActionMouseDoubleClick,
				Button: ButtonLeft,
				X:      150,
				Y:      250,
				HWND:   hwnd,
			},
		},
		{
			name: "mouse drag",
			payload: &gw.ControlRequestPayload{
				Kind:   gw.OperationKindMouseDrag,
				Button: "right",
				FromX:  10,
				FromY:  20,
				ToX:    30,
				ToY:    40,
			},
			want: Command{
				Action: ActionMouseDrag,
				Button: ButtonRight,
				FromX:  10,
				FromY:  20,
				ToX:    30,
				ToY:    40,
				HWND:   hwnd,
			},
		},
		{
			name: "mouse hover",
			payload: &gw.ControlRequestPayload{
				Kind:   gw.OperationKindMouseHover,
				Button: "left",
				X:      500,
				Y:      600,
			},
			want: Command{
				Action: ActionMouseHover,
				Button: ButtonLeft,
				X:      500,
				Y:      600,
				HWND:   hwnd,
			},
		},
		{
			name: "mouse hold",
			payload: &gw.ControlRequestPayload{
				Kind:       gw.OperationKindMouseHold,
				Button:     "middle",
				X:          400,
				Y:          300,
				DurationMs: 5000,
			},
			want: Command{
				Action:     ActionMouseHold,
				Button:     ButtonMiddle,
				X:          400,
				Y:          300,
				DurationMS: 5000,
				HWND:       hwnd,
			},
		},
		{
			name:    "nil payload",
			payload: nil,
			wantErr: true,
		},
		{
			name: "unsupported operation kind",
			payload: &gw.ControlRequestPayload{
				Kind:   "unknown",
				Button: "left",
			},
			wantErr: true,
		},
		{
			name: "unsupported button",
			payload: &gw.ControlRequestPayload{
				Kind:   gw.OperationKindMouseClick,
				Button: "unknown",
			},
			wantErr: true,
		},
		{
			name: "hold duration zero",
			payload: &gw.ControlRequestPayload{
				Kind:       gw.OperationKindMouseHold,
				Button:     "left",
				X:          100,
				Y:          200,
				DurationMs: 0,
			},
			wantErr: true,
		},
		{
			name: "hold duration exceeds max",
			payload: &gw.ControlRequestPayload{
				Kind:       gw.OperationKindMouseHold,
				Button:     "left",
				X:          100,
				Y:          200,
				DurationMs: 60000,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			got, err := ConvertControlRequest(tt.payload, hwnd)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("ConvertControlRequest() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ConvertControlRequest() unexpected error: %v", err)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("ConvertControlRequest() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestConvertControlRequest_DragFields(t *testing.T) {
	payload := &gw.ControlRequestPayload{
		Kind:   gw.OperationKindMouseDrag,
		Button: "left",
		FromX:  10,
		FromY:  20,
		ToX:    300,
		ToY:    400,
	}

	got, err := ConvertControlRequest(payload, 0)
	if err != nil {
		t.Fatalf("ConvertControlRequest() unexpected error: %v", err)
	}

	if got.FromX != 10 || got.FromY != 20 || got.ToX != 300 || got.ToY != 400 {
		t.Fatalf("drag fields: from_x=%d, from_y=%d, to_x=%d, to_y=%d; want 10,20,300,400",
			got.FromX, got.FromY, got.ToX, got.ToY)
	}
	// Drag should not set X/Y.
	if got.X != 0 || got.Y != 0 {
		t.Fatalf("drag should not set x/y: x=%d, y=%d", got.X, got.Y)
	}
}

func TestConvertControlRequest_HoldDurationMS(t *testing.T) {
	payload := &gw.ControlRequestPayload{
		Kind:       gw.OperationKindMouseHold,
		Button:     "left",
		X:          100,
		Y:          200,
		DurationMs: 10000,
	}

	got, err := ConvertControlRequest(payload, 0)
	if err != nil {
		t.Fatalf("ConvertControlRequest() unexpected error: %v", err)
	}

	if got.DurationMS != 10000 {
		t.Fatalf("duration_ms = %d, want 10000", got.DurationMS)
	}
}

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    Response
		wantErr bool
	}{
		{
			name: "ok response",
			data: `{"status":"ok"}`,
			want: Response{Status: StatusOK},
		},
		{
			name: "ok response with newline",
			data: `{"status":"ok"}` + "\n",
			want: Response{Status: StatusOK},
		},
		{
			name: "error response",
			data: `{"status":"error","message":"missing action"}`,
			want: Response{Status: StatusError, Message: "missing action"},
		},
		{
			name:    "invalid json",
			data:    `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			got, err := ParseResponse([]byte(tt.data))

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("ParseResponse() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ParseResponse() unexpected error: %v", err)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("ParseResponse() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTimeoutForAction(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		want   time.Duration
	}{
		{name: "click uses default", action: ActionMouseClick, want: DefaultTimeout},
		{name: "double click uses default", action: ActionMouseDoubleClick, want: DefaultTimeout},
		{name: "hover uses default", action: ActionMouseHover, want: DefaultTimeout},
		{name: "drag uses drag timeout", action: ActionMouseDrag, want: DragTimeout},
		{name: "hold uses max hold duration", action: ActionMouseHold, want: MaxHoldDuration},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			got := timeoutForAction(tt.action)

			// then
			if got != tt.want {
				t.Fatalf("timeoutForAction(%s) = %v, want %v", tt.action, got, tt.want)
			}
		})
	}
}

func TestCommandJSON(t *testing.T) {
	tests := []struct {
		name     string
		cmd      Command
		wantJSON string
	}{
		{
			name: "click command",
			cmd: Command{
				Action: ActionMouseClick,
				Button: ButtonLeft,
				X:      100,
				Y:      200,
				HWND:   12345,
			},
			wantJSON: `{"action":"mouse_click","button":"left","x":100,"y":200,"hwnd":12345}`,
		},
		{
			name: "drag command",
			cmd: Command{
				Action: ActionMouseDrag,
				Button: ButtonRight,
				FromX:  10,
				FromY:  20,
				ToX:    300,
				ToY:    400,
				HWND:   99,
			},
			wantJSON: `{"action":"mouse_drag","button":"right","from_x":10,"from_y":20,"to_x":300,"to_y":400,"hwnd":99}`,
		},
		{
			name: "hold command",
			cmd: Command{
				Action:     ActionMouseHold,
				Button:     ButtonMiddle,
				X:          50,
				Y:          60,
				DurationMS: 5000,
				HWND:       1,
			},
			wantJSON: `{"action":"mouse_hold","button":"middle","x":50,"y":60,"duration_ms":5000,"hwnd":1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			got, err := json.Marshal(tt.cmd)

			// then
			if err != nil {
				t.Fatalf("json.Marshal() unexpected error: %v", err)
			}
			if string(got) != tt.wantJSON {
				t.Fatalf("json.Marshal() = %s, want %s", got, tt.wantJSON)
			}
		})
	}
}

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatalf("NewManager() returned nil")
	}
	if m.Running() {
		t.Fatalf("new manager should not be running")
	}
}

func TestManager_StopNotStarted(t *testing.T) {
	m := NewManager()
	err := m.Stop()
	if err != nil {
		t.Fatalf("Stop() on unstarted manager returned error: %v", err)
	}
}
