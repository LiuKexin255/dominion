package main

import (
	"strings"
	"testing"
)

func TestParseOptions_Validate(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantCmd string
		wantErr bool
	}{
		{
			name:    "validate with target",
			args:    []string{"validate", "plan.yaml"},
			wantCmd: "validate",
		},
		{
			name:    "validate with timeout flag",
			args:    []string{"validate", "--timeout=10m", "plan.yaml"},
			wantCmd: "validate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parseOptions(tt.args)
			if tt.wantErr && err == nil {
				t.Fatalf("parseOptions(%v) expected error", tt.args)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("parseOptions(%v) unexpected error: %v", tt.args, err)
			}
			if !tt.wantErr && opts.command != tt.wantCmd {
				t.Fatalf("parseOptions(%v) command = %q, want %q", tt.args, opts.command, tt.wantCmd)
			}
		})
	}
}

func TestParseOptions_Run(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name: "run with target",
			args: []string{"run", "plan.yaml"},
		},
		{
			name: "run with timeout flag",
			args: []string{"run", "--timeout=5m", "plan.yaml"},
		},
		{
			name:    "run without target",
			args:    []string{"run"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOptions(tt.args)
			if tt.wantErr && err == nil {
				t.Fatalf("parseOptions(%v) expected error", tt.args)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("parseOptions(%v) unexpected error: %v", tt.args, err)
			}
		})
	}
}

func TestParseOptions_NoArgs(t *testing.T) {
	_, err := parseOptions(nil)
	if err == nil {
		t.Fatal("parseOptions(nil) expected error")
	}
}

func TestParseOptions_UnknownCommand(t *testing.T) {
	_, err := parseOptions([]string{"foobar", "plan.yaml"})
	if err == nil {
		t.Fatal("parseOptions with unknown command expected error")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("error should mention unknown command, got: %v", err)
	}
}

func TestRunCLI_Help(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "long help", args: []string{"--help"}},
		{name: "short help", args: []string{"-h"}},
		{name: "help keyword", args: []string{"help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			origStdout := stdout
			stdout = &buf
			defer func() { stdout = origStdout }()

			err := runCLI(tt.args)
			if err != nil {
				t.Fatalf("runCLI(%v) unexpected error: %v", tt.args, err)
			}
			output := buf.String()
			if !strings.Contains(output, "Usage: guitar") {
				t.Fatalf("help output should contain usage, got: %q", output)
			}
			if !strings.Contains(output, "validate") || !strings.Contains(output, "run") {
				t.Fatalf("help output should list commands, got: %q", output)
			}
		})
	}
}
