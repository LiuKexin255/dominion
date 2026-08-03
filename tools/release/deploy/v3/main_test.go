package main

import (
	"bytes"
	"os"
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
			name: "apply with endpoint timeout",
			args: []string{"apply", "--endpoint=http://localhost:8081", "--timeout=30s", "deploy.yaml"},
			want: &options{
				command:  commandApply,
				target:   "deploy.yaml",
				endpoint: "http://localhost:8081",
				timeout:  30 * time.Second,
			},
		},
		{
			name: "apply with run flag",
			args: []string{"apply", "--endpoint=http://localhost:8081", "--timeout=30s", "--run=abc123", "deploy.yaml"},
			want: &options{
				command:  commandApply,
				target:   "deploy.yaml",
				endpoint: "http://localhost:8081",
				timeout:  30 * time.Second,
				run:      "abc123",
			},
		},
		{
			name:    "del does not accept run flag",
			args:    []string{"del", "--run=abc123", "team.dev"},
			wantErr: true,
		},
		// US2 验收场景 1（specs/033-deploy-scope-cleanup/spec.md:61）：
		// del 传 --scope 须返回 flag 解析错误。
		{name: "del rejects --scope", args: []string{"del", "--scope=team", "alice.dev"}, wantErr: true},
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
		{name: "unknown command", args: []string{"use", "team.dev"}, wantErr: true},
		// US1 验收场景 1（specs/033-deploy-scope-cleanup/spec.md:46）：
		// scope 命令已移除，返回 unknown command 错误。
		{name: "scope command removed", args: []string{"scope"}, wantErr: true},
		{name: "apply missing target", args: []string{"apply"}, wantErr: true},
		{name: "delete missing target", args: []string{"del"}, wantErr: true},
		{name: "list positional arg rejected", args: []string{"list", "team"}, wantErr: true},
		// R5（specs/033-deploy-scope-cleanup/research.md:92）：--scope 值须匹配
		// envPartRegexp（^[a-z][a-z0-9]{0,7}$），非法值与 "-" 通配符均被拒绝。
		{name: "list invalid scope rejected", args: []string{"list", "--scope=INVALID"}, wantErr: true},
		{name: "list wildcard scope rejected", args: []string{"list", "--scope=-"}, wantErr: true},
		{
			name: "apply with verbose flag",
			args: []string{"apply", "-v", "--endpoint=http://localhost:8081", "--timeout=30s", "deploy.yaml"},
			want: &options{
				command:  commandApply,
				target:   "deploy.yaml",
				endpoint: "http://localhost:8081",
				timeout:  30 * time.Second,
				verbose:  true,
			},
		},
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
	if !strings.Contains(got, "Usage: deploy_v3 <command> [args]") {
		t.Fatalf("run(--help) output = %q, want v3 usage", got)
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
	if !strings.Contains(got, "--verbose") {
		t.Fatalf("run(--help) output = %q, want verbose flag", got)
	}
	if strings.Contains(got, "  scope ") {
		t.Fatalf("run(--help) output = %q, want no scope command", got)
	}
}

// withWorkingDir 切换当前工作目录并在测试结束后恢复。
func withWorkingDir(t *testing.T, dir string) {
	t.Helper()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore working dir failed: %v", err)
		}
	})
}
