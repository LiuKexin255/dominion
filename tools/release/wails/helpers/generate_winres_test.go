package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tc-hib/winres"
)

func TestGenerateWinres(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) (icon string, manifest string, info string)
		arch    string
		wantErr bool
		check   func(t *testing.T, outputPath string)
	}{
		{
			name: "minimal syso with no inputs",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				return "", "", ""
			},
			arch:    "amd64",
			wantErr: false,
			check: func(t *testing.T, outputPath string) {
				t.Helper()
				stat, err := os.Stat(outputPath)
				if err != nil {
					t.Fatalf("output file does not exist: %v", err)
				}
				if stat.Size() == 0 {
					t.Fatalf("output file is empty")
				}
			},
		},
		{
			name: "syso with icon only",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				icon := testdataPath(t, "icon.ico")
				return icon, "", ""
			},
			arch:    "amd64",
			wantErr: false,
			check: func(t *testing.T, outputPath string) {
				t.Helper()
				data, err := os.ReadFile(outputPath)
				if err != nil {
					t.Fatalf("read output: %v", err)
				}
				if len(data) == 0 {
					t.Fatalf("output is empty")
				}
			},
		},
		{
			name: "syso with manifest only",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				manifest := testdataPath(t, "test.manifest")
				return "", manifest, ""
			},
			arch:    "amd64",
			wantErr: false,
			check: func(t *testing.T, outputPath string) {
				t.Helper()
				stat, err := os.Stat(outputPath)
				if err != nil {
					t.Fatalf("output file does not exist: %v", err)
				}
				if stat.Size() == 0 {
					t.Fatalf("output file is empty")
				}
			},
		},
		{
			name: "syso with icon and manifest",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				icon := testdataPath(t, "icon.ico")
				manifest := testdataPath(t, "test.manifest")
				return icon, manifest, ""
			},
			arch:    "amd64",
			wantErr: false,
			check: func(t *testing.T, outputPath string) {
				t.Helper()
				stat, err := os.Stat(outputPath)
				if err != nil {
					t.Fatalf("output file does not exist: %v", err)
				}
				if stat.Size() == 0 {
					t.Fatalf("output file is empty")
				}
			},
		},
		{
			name: "syso with version info",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				infoPath := writeTestInfoJSON(t)
				return "", "", infoPath
			},
			arch:    "amd64",
			wantErr: false,
			check: func(t *testing.T, outputPath string) {
				t.Helper()
				stat, err := os.Stat(outputPath)
				if err != nil {
					t.Fatalf("output file does not exist: %v", err)
				}
				if stat.Size() == 0 {
					t.Fatalf("output file is empty")
				}
			},
		},
		{
			name: "syso with all inputs",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				icon := testdataPath(t, "icon.ico")
				manifest := testdataPath(t, "test.manifest")
				infoPath := writeTestInfoJSON(t)
				return icon, manifest, infoPath
			},
			arch:    "amd64",
			wantErr: false,
			check: func(t *testing.T, outputPath string) {
				t.Helper()
				stat, err := os.Stat(outputPath)
				if err != nil {
					t.Fatalf("output file does not exist: %v", err)
				}
				if stat.Size() == 0 {
					t.Fatalf("output file is empty")
				}
			},
		},
		{
			name: "arm64 architecture",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				return "", "", ""
			},
			arch:    "arm64",
			wantErr: false,
			check: func(t *testing.T, outputPath string) {
				t.Helper()
				stat, err := os.Stat(outputPath)
				if err != nil {
					t.Fatalf("output file does not exist: %v", err)
				}
				if stat.Size() == 0 {
					t.Fatalf("output file is empty")
				}
			},
		},
		{
			name: "386 architecture",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				return "", "", ""
			},
			arch:    "386",
			wantErr: false,
			check: func(t *testing.T, outputPath string) {
				t.Helper()
				stat, err := os.Stat(outputPath)
				if err != nil {
					t.Fatalf("output file does not exist: %v", err)
				}
				if stat.Size() == 0 {
					t.Fatalf("output file is empty")
				}
			},
		},
		{
			name: "unsupported architecture",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				return "", "", ""
			},
			arch:    "sparc",
			wantErr: true,
		},
		{
			name: "nonexistent icon file",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				return filepath.Join(t.TempDir(), "nonexistent.ico"), "", ""
			},
			arch:    "amd64",
			wantErr: true,
		},
		{
			name: "nonexistent manifest file",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				return "", filepath.Join(t.TempDir(), "nonexistent.manifest"), ""
			},
			arch:    "amd64",
			wantErr: true,
		},
		{
			name: "nonexistent info file",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				return "", "", filepath.Join(t.TempDir(), "nonexistent.json")
			},
			arch:    "amd64",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			icon, manifest, info := tt.setup(t)
			outputPath := filepath.Join(t.TempDir(), "resource_windows_amd64.syso")

			// when
			err := generateWinres(icon, manifest, info, tt.arch, outputPath)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil && err == nil {
				tt.check(t, outputPath)
			}
		})
	}
}

func TestSetVersionInfo(t *testing.T) {
	tests := []struct {
		name    string
		info    map[string]string
		wantErr bool
	}{
		{
			name: "all fields populated",
			info: map[string]string{
				"FileVersion":     "1.2.3.4",
				"ProductVersion":  "1.2.3.4",
				"CompanyName":     "Test Corp",
				"FileDescription": "Test Application",
				"ProductName":     "Test Product",
				"LegalCopyright":  "Copyright 2024",
			},
			wantErr: false,
		},
		{
			name: "partial fields",
			info: map[string]string{
				"FileVersion": "2.0.0",
				"ProductName": "Minimal App",
			},
			wantErr: false,
		},
		{
			name:    "empty json object",
			info:    map[string]string{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rs := winres.ResourceSet{}
			infoPath := filepath.Join(t.TempDir(), "info.json")
			data, err := json.Marshal(tt.info)
			if err != nil {
				t.Fatalf("marshal info: %v", err)
			}
			if err := os.WriteFile(infoPath, data, 0o644); err != nil {
				t.Fatalf("write info file: %v", err)
			}

			// when
			err = setVersionInfo(&rs, infoPath)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestArchMapping(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "amd64 exists", input: "amd64", want: true},
		{name: "arm64 exists", input: "arm64", want: true},
		{name: "386 exists", input: "386", want: true},
		{name: "arm exists", input: "arm", want: true},
		{name: "unknown arch", input: "unknown", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given + when
			_, ok := archMapping[tt.input]

			// then
			if ok != tt.want {
				t.Fatalf("archMapping[%q] existence = %v, want %v", tt.input, ok, tt.want)
			}
		})
	}
}

// testdataPath returns the absolute path to a file in the testdata directory.
func testdataPath(t *testing.T, filename string) string {
	t.Helper()
	return filepath.Join("testdata", filename)
}

// writeTestInfoJSON creates a temporary info.json file with test version info.
func writeTestInfoJSON(t *testing.T) string {
	t.Helper()
	info := map[string]string{
		"FileVersion":     "1.0.0.0",
		"ProductVersion":  "1.0.0.0",
		"CompanyName":     "Test Company",
		"FileDescription": "Test Application",
		"ProductName":     "Test Product",
		"LegalCopyright":  "Copyright 2024 Test Company",
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		t.Fatalf("marshal info: %v", err)
	}
	path := filepath.Join(t.TempDir(), "info.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write info file: %v", err)
	}
	return path
}
