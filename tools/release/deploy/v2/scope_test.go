package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		seed    *cliConfig
		want    *cliConfig
		wantErr bool
	}{
		{name: "missing file returns empty config", want: &cliConfig{}},
		{name: "existing file loads default scope", seed: &cliConfig{DefaultScope: "team"}, want: &cliConfig{DefaultScope: "team"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newScopeWorkspace(t)
			if tt.seed != nil {
				if err := saveConfig(root, tt.seed); err != nil {
					t.Fatalf("saveConfig() setup failed: %v", err)
				}
			}

			got, err := loadConfig(root)
			if tt.wantErr {
				if err == nil {
					t.Fatal("loadConfig() succeeded unexpectedly")
				}
				return
			}
			if err != nil {
				t.Fatalf("loadConfig() failed: %v", err)
			}
			if got.DefaultScope != tt.want.DefaultScope {
				t.Fatalf("loadConfig() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSaveConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *cliConfig
		want *cliConfig
	}{
		{name: "writes default scope", cfg: &cliConfig{DefaultScope: "team"}, want: &cliConfig{DefaultScope: "team"}},
		{name: "writes empty config", cfg: &cliConfig{}, want: &cliConfig{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newScopeWorkspace(t)
			if err := saveConfig(root, tt.cfg); err != nil {
				t.Fatalf("saveConfig() failed: %v", err)
			}

			raw, err := os.ReadFile(filepath.Join(root, ".env", "cli.json"))
			if err != nil {
				t.Fatalf("os.ReadFile() failed: %v", err)
			}

			got := new(cliConfig)
			if err := json.Unmarshal(raw, got); err != nil {
				t.Fatalf("json.Unmarshal() failed: %v", err)
			}
			if got.DefaultScope != tt.want.DefaultScope {
				t.Fatalf("saved config = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestScopeCommand_DisplayDefaultScope(t *testing.T) {
	tests := []struct {
		name       string
		seed       *cliConfig
		wantOutput string
	}{
		{name: "unset scope", wantOutput: "not set"},
		{name: "existing scope", seed: &cliConfig{DefaultScope: "team"}, wantOutput: "team"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, cwd := newScopeRepo(t)
			withWorkingDir(t, cwd)
			if tt.seed != nil {
				if err := saveConfig(root, tt.seed); err != nil {
					t.Fatalf("saveConfig() setup failed: %v", err)
				}
			}

			gotOutput := captureStdout(t, func() error {
				return scopeCommand(&options{})
			})

			if strings.TrimSpace(gotOutput) != tt.wantOutput {
				t.Fatalf("scopeCommand() output = %q, want %q", strings.TrimSpace(gotOutput), tt.wantOutput)
			}
		})
	}
}

func TestScopeCommand_SetDefaultScope(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		wantOutput string
	}{
		{name: "sets team scope", target: "team", wantOutput: "默认 scope 已设置为 team"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, cwd := newScopeRepo(t)
			withWorkingDir(t, cwd)

			gotOutput := captureStdout(t, func() error {
				return scopeCommand(&options{target: tt.target})
			})

			if strings.TrimSpace(gotOutput) != tt.wantOutput {
				t.Fatalf("scopeCommand() output = %q, want %q", strings.TrimSpace(gotOutput), tt.wantOutput)
			}

			cfg, err := loadConfig(root)
			if err != nil {
				t.Fatalf("loadConfig() failed: %v", err)
			}
			if cfg.DefaultScope != tt.target {
				t.Fatalf("DefaultScope = %q, want %q", cfg.DefaultScope, tt.target)
			}
		})
	}
}

func TestScopeCommand_RejectsInvalidScope(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{name: "uppercase scope", target: "TEAM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, cwd := newScopeRepo(t)
			withWorkingDir(t, cwd)

			err := scopeCommand(&options{target: tt.target})
			if err == nil {
				t.Fatal("scopeCommand() succeeded unexpectedly")
			}
			if !strings.Contains(err.Error(), "非法 scope") {
				t.Fatalf("scopeCommand() error = %v, want validation error", err)
			}

			if _, err := os.Stat(filepath.Join(root, ".env", "cli.json")); !os.IsNotExist(err) {
				t.Fatalf("cli.json exists unexpectedly: %v", err)
			}
		})
	}
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()

	oldStdout := stdout
	var out bytes.Buffer
	stdout = &out
	t.Cleanup(func() { stdout = oldStdout })

	callErr := fn()
	if callErr != nil {
		t.Fatalf("call failed: %v", callErr)
	}
	return out.String()
}

func newScopeRepo(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	cwd := filepath.Join(root, "apps", "svc")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	return root, cwd
}

func newScopeWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	return root
}

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
