package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspect(t *testing.T) {
	tests := []struct {
		name        string
		config      string
		wantErrText string
	}{
		{
			name: "valid config with empty frontend commands",
			config: `{
				"name": "demo",
				"outputfilename": "demo",
				"frontend:install": "",
				"frontend:build": "",
				"frontend:dev:watcher": "pnpm dev",
				"frontend:dev:serverUrl": "http://localhost:5173",
				"author": {"name": "Dev", "email": "dev@example.com"}
			}`,
		},
		{
			name: "invalid config with frontend install",
			config: `{
				"name": "demo",
				"frontend:install": "pnpm install",
				"frontend:build": ""
			}`,
			wantErrText: "frontend:install must be empty",
		},
		{
			name: "invalid config with frontend build",
			config: `{
				"name": "demo",
				"frontend:install": "",
				"frontend:build": "pnpm build"
			}`,
			wantErrText: "frontend:build must be empty",
		},
		{
			name: "config with hooks",
			config: `{
				"name": "demo",
				"frontend:install": "",
				"frontend:build": "",
				"hooks": {"preBuild": "go generate ./..."}
			}`,
			wantErrText: "hooks must be empty or absent",
		},
		{
			name:        "invalid json",
			config:      `{"name":`,
			wantErrText: "parse wails.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "wails.json")
			outPath := filepath.Join(tmpDir, "validated")
			if err := os.WriteFile(configPath, []byte(tt.config), 0644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			oldArgs := os.Args
			oldCommandLine := flag.CommandLine
			defer func() {
				os.Args = oldArgs
				flag.CommandLine = oldCommandLine
			}()

			os.Args = []string{"inspect_wails_config", "-wails_json", configPath, "-out", outPath}
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

			err := inspect()
			if tt.wantErrText != "" {
				if err == nil {
					t.Fatalf("inspect() expected error containing %q", tt.wantErrText)
				}
				if !strings.Contains(err.Error(), tt.wantErrText) {
					t.Fatalf("inspect() error = %q, want substring %q", err.Error(), tt.wantErrText)
				}
				if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
					t.Fatalf("output marker exists after failed inspect: %v", statErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("inspect() unexpected error: %v", err)
			}
			info, statErr := os.Stat(outPath)
			if statErr != nil {
				t.Fatalf("output marker missing: %v", statErr)
			}
			if info.Size() != 0 {
				t.Fatalf("output marker size = %d, want 0", info.Size())
			}
		})
	}
}

func TestInspect_missingFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "missing-wails.json")
	outPath := filepath.Join(tmpDir, "validated")

	oldArgs := os.Args
	oldCommandLine := flag.CommandLine
	defer func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
	}()

	os.Args = []string{"inspect_wails_config", "-wails_json", configPath, "-out", outPath}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	err := inspect()
	if err == nil {
		t.Fatal("inspect() expected missing file error")
	}
	if !strings.Contains(err.Error(), "read wails.json") {
		t.Fatalf("inspect() error = %q, want read wails.json", err.Error())
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("output marker exists after failed inspect: %v", statErr)
	}
}
