package main

import (
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      Command
		wantErr   bool
		errSubstr string
	}{
		{
			name:  "valid click",
			input: `{"action":"mouse_click","button":"left","x":100,"y":200}`,
			want: Command{
				Action: ActionMouseClick,
				Button: ButtonLeft,
				X:      100,
				Y:      200,
			},
		},
		{
			name:  "valid double click",
			input: `{"action":"mouse_double_click","button":"right","x":100,"y":200}`,
			want: Command{
				Action: ActionMouseDoubleClick,
				Button: ButtonRight,
				X:      100,
				Y:      200,
			},
		},
		{
			name:  "valid hover ignores button",
			input: `{"action":"mouse_hover","button":"left","x":100,"y":200}`,
			want: Command{
				Action: ActionMouseHover,
				Button: ButtonLeft,
				X:      100,
				Y:      200,
			},
		},
		{
			name:  "valid drag with all coordinates",
			input: `{"action":"mouse_drag","button":"middle","from_x":10,"from_y":20,"to_x":100,"to_y":200,"duration_ms":500}`,
			want: Command{
				Action:     ActionMouseDrag,
				Button:     ButtonMiddle,
				FromX:      10,
				FromY:      20,
				ToX:        100,
				ToY:        200,
				DurationMS: 500,
			},
		},
		{
			name:  "valid hold",
			input: `{"action":"mouse_hold","button":"left","x":100,"y":200,"duration_ms":3000}`,
			want: Command{
				Action:     ActionMouseHold,
				Button:     ButtonLeft,
				X:          100,
				Y:          200,
				DurationMS: 3000,
			},
		},
		{
			name:      "invalid json",
			input:     `{"action":`,
			wantErr:   true,
			errSubstr: "invalid json",
		},
		{
			name:      "invalid action",
			input:     `{"action":"unknown","button":"left","x":100,"y":200}`,
			wantErr:   true,
			errSubstr: "invalid action: unknown",
		},
		{
			name:      "missing action",
			input:     `{"button":"left","x":100,"y":200}`,
			wantErr:   true,
			errSubstr: "missing action",
		},
		{
			name:      "invalid button",
			input:     `{"action":"mouse_click","button":"side","x":100,"y":200}`,
			wantErr:   true,
			errSubstr: "invalid button: side",
		},
		{
			name:      "missing button",
			input:     `{"action":"mouse_click","x":100,"y":200}`,
			wantErr:   true,
			errSubstr: "missing button",
		},
		{
			name:      "missing x",
			input:     `{"action":"mouse_click","button":"left","y":200}`,
			wantErr:   true,
			errSubstr: "missing x",
		},
		{
			name:      "negative coordinate",
			input:     `{"action":"mouse_click","button":"left","x":-1,"y":200}`,
			wantErr:   true,
			errSubstr: "x must be non-negative",
		},
		{
			name:      "drag missing from y",
			input:     `{"action":"mouse_drag","button":"left","from_x":10,"to_x":100,"to_y":200,"duration_ms":500}`,
			wantErr:   true,
			errSubstr: "missing from_y",
		},
		{
			name:      "drag negative target coordinate",
			input:     `{"action":"mouse_drag","button":"left","from_x":10,"from_y":20,"to_x":-100,"to_y":200,"duration_ms":500}`,
			wantErr:   true,
			errSubstr: "to_x must be non-negative",
		},
		{
			name:      "hold missing duration",
			input:     `{"action":"mouse_hold","button":"left","x":100,"y":200}`,
			wantErr:   true,
			errSubstr: "missing duration_ms",
		},
		{
			name:      "hold duration too long",
			input:     `{"action":"mouse_hold","button":"left","x":100,"y":200,"duration_ms":30001}`,
			wantErr:   true,
			errSubstr: "duration_ms must be <= 30000",
		},
		{
			name:      "negative duration",
			input:     `{"action":"mouse_hold","button":"left","x":100,"y":200,"duration_ms":-1}`,
			wantErr:   true,
			errSubstr: "duration_ms must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := ParseCommand([]byte(tt.input))

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseCommand(%s) expected error", tt.input)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("ParseCommand(%s) error = %v, want substring %q", tt.input, err, tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCommand(%s) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("ParseCommand(%s) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestHandleCommand(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		execute   func(*Command) error
		want      Response
		wantCalls int
	}{
		{
			name:  "returns ok when executor succeeds",
			input: `{"action":"mouse_hover","button":"left","x":100,"y":200}`,
			execute: func(command *Command) error {
				if command.Action != ActionMouseHover {
					t.Fatalf("command.Action = %q, want %q", command.Action, ActionMouseHover)
				}
				return nil
			},
			want:      Response{Status: StatusOK},
			wantCalls: 1,
		},
		{
			name:  "returns validation error without calling executor",
			input: `{"action":"unknown","button":"left","x":100,"y":200}`,
			want: Response{
				Status:  StatusError,
				Message: "invalid action: unknown",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			executor := ExecutorFunc(func(command *Command) error {
				calls++
				if tt.execute == nil {
					return nil
				}
				return tt.execute(command)
			})

			// when
			got := HandleCommand([]byte(tt.input), executor)

			// then
			if got != tt.want {
				t.Fatalf("HandleCommand(%s) = %+v, want %+v", tt.input, got, tt.want)
			}
			if calls != tt.wantCalls {
				t.Fatalf("executor calls = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}
