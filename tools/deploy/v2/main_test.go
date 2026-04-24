package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func Test_parseOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    *options
		wantErr bool
	}{
		{
			name: "apply with endpoint timeout and scope",
			args: []string{"apply", "--endpoint=http://localhost:8081", "--timeout=30s", "--scope=team", "deploy.yaml"},
			want: &options{
				command:  commandApply,
				target:   "deploy.yaml",
				endpoint: "http://localhost:8081",
				timeout:  30 * time.Second,
				scope:    "team",
			},
		},
		{
			name: "apply with run flag",
			args: []string{"apply", "--endpoint=http://localhost:8081", "--timeout=30s", "--scope=team", "--run=abc123", "deploy.yaml"},
			want: &options{
				command:  commandApply,
				target:   "deploy.yaml",
				endpoint: "http://localhost:8081",
				timeout:  30 * time.Second,
				scope:    "team",
				run:      "abc123",
			},
		},
		{
			name: "apply with run flag empty default",
			args: []string{"apply", "--endpoint=http://localhost:8081", "--timeout=30s", "--scope=team", "deploy.yaml"},
			want: &options{
				command:  commandApply,
				target:   "deploy.yaml",
				endpoint: "http://localhost:8081",
				timeout:  30 * time.Second,
				scope:    "team",
				run:      "",
			},
		},
		{
			name:    "del does not accept run flag",
			args:    []string{"del", "--run=abc123", "team.dev"},
			wantErr: true,
		},
		{
			name: "delete target",
			args: []string{"del", "team.dev"},
			want: &options{
				command:  commandDel,
				target:   "team.dev",
				endpoint: defaultEndpoint,
				timeout:  defaultTimeout,
			},
		},
		{
			name: "list scope flag",
			args: []string{"list", "--scope=team"},
			want: &options{
				command:  commandList,
				endpoint: defaultEndpoint,
				timeout:  defaultTimeout,
				scope:    "team",
			},
		},
		{
			name: "scope target",
			args: []string{"scope", "team"},
			want: &options{
				command:  commandScope,
				target:   "team",
				endpoint: defaultEndpoint,
				timeout:  defaultTimeout,
			},
		},
		{name: "unknown command", args: []string{"use", "team.dev"}, wantErr: true},
		{name: "apply missing target", args: []string{"apply"}, wantErr: true},
		{name: "delete missing target", args: []string{"del"}, wantErr: true},
		{name: "list positional arg rejected", args: []string{"list", "team"}, wantErr: true},
		{name: "scope invalid target", args: []string{"scope", "TEAM"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOptions(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseOptions(%v) expected error", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseOptions(%v) unexpected error: %v", tt.args, err)
			}
			if *got != *tt.want {
				t.Fatalf("parseOptions(%v) = %#v, want %#v", tt.args, got, tt.want)
			}
		})
	}
}

func TestRun_Help(t *testing.T) {
	var out bytes.Buffer
	oldStdout := stdout
	stdout = &out
	t.Cleanup(func() { stdout = oldStdout })

	if err := run([]string{"--help"}); err != nil {
		t.Fatalf("run(--help) unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Usage: deploy <command> [args]") {
		t.Fatalf("run(--help) output = %q, want usage", got)
	}
	if !strings.Contains(got, "--endpoint") {
		t.Fatalf("run(--help) output = %q, want endpoint flag", got)
	}
	if !strings.Contains(got, "--timeout") {
		t.Fatalf("run(--help) output = %q, want timeout flag", got)
	}
	if !strings.Contains(got, "--run") {
		t.Fatalf("run(--help) output = %q, want run flag", got)
	}
}
