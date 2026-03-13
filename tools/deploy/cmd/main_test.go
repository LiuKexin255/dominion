package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "env only", args: []string{"--env=dev"}},
		{name: "deploy with env", args: []string{"--deploy=deploy.yaml", "--env=dev"}},
		{name: "delete only", args: []string{"--del=dev"}},
		{name: "missing args", args: nil, wantErr: true},
		{name: "deploy without env", args: []string{"--deploy=deploy.yaml"}, wantErr: true},
		{name: "delete with env", args: []string{"--del=dev", "--env=dev"}, wantErr: true},
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

func TestEnvCache(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	tmpDir := t.TempDir()
	t.Setenv("BUILD_WORKSPACE_DIRECTORY", tmpDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	tests := []struct {
		name        string
		env         string
		prepare     func(t *testing.T)
		wantCreated bool
		wantDeleted bool
	}{
		{
			name:        "create new env cache",
			env:         "dev",
			wantCreated: true,
			wantDeleted: true,
		},
		{
			name: "switch existing env cache",
			env:  "staging",
			prepare: func(t *testing.T) {
				t.Helper()
				if _, _, err := upsertEnvCache("staging"); err != nil {
					t.Fatalf("prepare env cache: %v", err)
				}
			},
			wantCreated: false,
			wantDeleted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.prepare != nil {
				tt.prepare(t)
			}

			created, state, err := upsertEnvCache(tt.env)
			if err != nil {
				t.Fatalf("upsertEnvCache(%q) unexpected error: %v", tt.env, err)
			}
			if created != tt.wantCreated {
				t.Fatalf("upsertEnvCache(%q) created = %v, want %v", tt.env, created, tt.wantCreated)
			}
			if state.Name != tt.env {
				t.Fatalf("state.Name = %q, want %q", state.Name, tt.env)
			}

			cacheFile := filepath.Join(tmpDir, ".env", tt.env+".env.json")
			if _, err := os.Stat(cacheFile); err != nil {
				t.Fatalf("expected cache file exists: %v", err)
			}

			deleted, err := deleteEnvCache(tt.env)
			if err != nil {
				t.Fatalf("deleteEnvCache(%q) unexpected error: %v", tt.env, err)
			}
			if deleted != tt.wantDeleted {
				t.Fatalf("deleteEnvCache(%q) deleted = %v, want %v", tt.env, deleted, tt.wantDeleted)
			}
		})
	}
}
