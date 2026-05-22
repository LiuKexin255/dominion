package input

import (
	"encoding/json"
	"testing"
	"time"

	runtimepb "dominion/projects/game/runtime"
)

func TestCommandFromMouseClick(t *testing.T) {
	hwnd := uintptr(12345)

	tests := []struct {
		name    string
		click   *runtimepb.GameMouseClick
		want    Command
		wantErr bool
	}{
		{
			name: "left click",
			click: &runtimepb.GameMouseClick{
				Button: runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
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
			name: "right click",
			click: &runtimepb.GameMouseClick{
				Button: runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_RIGHT,
				X:      50,
				Y:      75,
			},
			want: Command{
				Action: ActionMouseClick,
				Button: ButtonRight,
				X:      50,
				Y:      75,
				HWND:   hwnd,
			},
		},
		{
			name:    "nil click",
			click:   nil,
			wantErr: true,
		},
		{
			name: "unsupported button",
			click: &runtimepb.GameMouseClick{
				Button: runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED,
				X:      10,
				Y:      20,
			},
			wantErr: true,
		},
		{
			name: "negative x",
			click: &runtimepb.GameMouseClick{
				Button: runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
				X:      -1,
				Y:      20,
			},
			wantErr: true,
		},
		{
			name: "negative y",
			click: &runtimepb.GameMouseClick{
				Button: runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
				X:      10,
				Y:      -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			got, err := CommandFromMouseClick(tt.click, hwnd)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("CommandFromMouseClick() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("CommandFromMouseClick() unexpected error: %v", err)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("CommandFromMouseClick() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCommandFromMouseDoubleClick(t *testing.T) {
	hwnd := uintptr(12345)

	tests := []struct {
		name    string
		dc      *runtimepb.GameMouseDoubleClick
		want    Command
		wantErr bool
	}{
		{
			name: "left double click",
			dc: &runtimepb.GameMouseDoubleClick{
				Button: runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
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
			name:    "nil double click",
			dc:      nil,
			wantErr: true,
		},
		{
			name: "unsupported button",
			dc: &runtimepb.GameMouseDoubleClick{
				Button: runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED,
				X:      10,
				Y:      20,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			got, err := CommandFromMouseDoubleClick(tt.dc, hwnd)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("CommandFromMouseDoubleClick() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("CommandFromMouseDoubleClick() unexpected error: %v", err)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("CommandFromMouseDoubleClick() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCommandFromMouseDrag(t *testing.T) {
	hwnd := uintptr(12345)

	tests := []struct {
		name    string
		drag    *runtimepb.GameMouseDrag
		want    Command
		wantErr bool
	}{
		{
			name: "right drag",
			drag: &runtimepb.GameMouseDrag{
				Button: runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_RIGHT,
				FromX:  10,
				FromY:  20,
				ToX:    300,
				ToY:    400,
			},
			want: Command{
				Action: ActionMouseDrag,
				Button: ButtonRight,
				FromX:  10,
				FromY:  20,
				ToX:    300,
				ToY:    400,
				HWND:   hwnd,
			},
		},
		{
			name:    "nil drag",
			drag:    nil,
			wantErr: true,
		},
		{
			name: "unsupported button",
			drag: &runtimepb.GameMouseDrag{
				Button: runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED,
				FromX:  10,
				FromY:  20,
				ToX:    30,
				ToY:    40,
			},
			wantErr: true,
		},
		{
			name: "negative from_x",
			drag: &runtimepb.GameMouseDrag{
				Button: runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
				FromX:  -1,
				FromY:  20,
				ToX:    30,
				ToY:    40,
			},
			wantErr: true,
		},
		{
			name: "negative to_y",
			drag: &runtimepb.GameMouseDrag{
				Button: runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
				FromX:  10,
				FromY:  20,
				ToX:    30,
				ToY:    -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			got, err := CommandFromMouseDrag(tt.drag, hwnd)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("CommandFromMouseDrag() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("CommandFromMouseDrag() unexpected error: %v", err)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("CommandFromMouseDrag() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCommandFromMouseHover(t *testing.T) {
	hwnd := uintptr(12345)

	tests := []struct {
		name    string
		hover   *runtimepb.GameMouseHover
		want    Command
		wantErr bool
	}{
		{
			name: "valid hover",
			hover: &runtimepb.GameMouseHover{
				X: 500,
				Y: 600,
			},
			want: Command{
				Action: ActionMouseHover,
				X:      500,
				Y:      600,
				HWND:   hwnd,
			},
		},
		{
			name:    "nil hover",
			hover:   nil,
			wantErr: true,
		},
		{
			name: "negative x",
			hover: &runtimepb.GameMouseHover{
				X: -1,
				Y: 600,
			},
			wantErr: true,
		},
		{
			name: "negative y",
			hover: &runtimepb.GameMouseHover{
				X: 500,
				Y: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			got, err := CommandFromMouseHover(tt.hover, hwnd)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("CommandFromMouseHover() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("CommandFromMouseHover() unexpected error: %v", err)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("CommandFromMouseHover() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCommandFromMouseHold(t *testing.T) {
	hwnd := uintptr(12345)

	tests := []struct {
		name    string
		hold    *runtimepb.GameMouseHold
		want    Command
		wantErr bool
	}{
		{
			name: "valid hold",
			hold: &runtimepb.GameMouseHold{
				Button:     runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_MIDDLE,
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
			name:    "nil hold",
			hold:    nil,
			wantErr: true,
		},
		{
			name: "unsupported button",
			hold: &runtimepb.GameMouseHold{
				Button:     runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_UNSPECIFIED,
				X:          100,
				Y:          200,
				DurationMs: 5000,
			},
			wantErr: true,
		},
		{
			name: "duration zero",
			hold: &runtimepb.GameMouseHold{
				Button:     runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
				X:          100,
				Y:          200,
				DurationMs: 0,
			},
			wantErr: true,
		},
		{
			name: "duration exceeds max",
			hold: &runtimepb.GameMouseHold{
				Button:     runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
				X:          100,
				Y:          200,
				DurationMs: 60000,
			},
			wantErr: true,
		},
		{
			name: "negative x",
			hold: &runtimepb.GameMouseHold{
				Button:     runtimepb.GameMouseButton_GAME_MOUSE_BUTTON_LEFT,
				X:          -1,
				Y:          200,
				DurationMs: 1000,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			got, err := CommandFromMouseHold(tt.hold, hwnd)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("CommandFromMouseHold() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("CommandFromMouseHold() unexpected error: %v", err)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("CommandFromMouseHold() = %+v, want %+v", got, tt.want)
			}
		})
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
