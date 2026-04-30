package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAssetsGo(t *testing.T) {
	tests := []struct {
		name        string
		varName     string
		embedDir    string
		packageName string
		wantErr     bool
		check       func(t *testing.T, outputFile string)
	}{
		{
			name:        "standard generation",
			varName:     "FrontendDist",
			embedDir:    "frontend_dist",
			packageName: "assets",
			wantErr:     false,
			check: func(t *testing.T, outputFile string) {
				t.Helper()
				got, err := os.ReadFile(outputFile)
				if err != nil {
					t.Fatalf("read output file: %v", err)
				}

				want := `package assets

import "embed"

//go:embed all:frontend_dist
var FrontendDist embed.FS
`
				if string(got) != want {
					t.Fatalf("output mismatch:\ngot:\n%s\nwant:\n%s", got, want)
				}
			},
		},
		{
			name:        "custom variable and directory",
			varName:     "StaticAssets",
			embedDir:    "static",
			packageName: "staticfiles",
			wantErr:     false,
			check: func(t *testing.T, outputFile string) {
				t.Helper()
				got, err := os.ReadFile(outputFile)
				if err != nil {
					t.Fatalf("read output file: %v", err)
				}

				content := string(got)
				if !strings.Contains(content, "package staticfiles") {
					t.Fatalf("missing package declaration, got:\n%s", content)
				}
				if !strings.Contains(content, "var StaticAssets embed.FS") {
					t.Fatalf("missing variable declaration, got:\n%s", content)
				}
				if !strings.Contains(content, "//go:embed all:static") {
					t.Fatalf("missing embed directive, got:\n%s", content)
				}
				if !strings.Contains(content, "import \"embed\"") {
					t.Fatalf("missing import, got:\n%s", content)
				}
			},
		},
		{
			name:        "empty variable name",
			varName:     "",
			embedDir:    "frontend_dist",
			packageName: "assets",
			wantErr:     true,
		},
		{
			name:        "empty embed directory",
			varName:     "FrontendDist",
			embedDir:    "",
			packageName: "assets",
			wantErr:     true,
		},
		{
			name:        "empty package name",
			varName:     "FrontendDist",
			embedDir:    "frontend_dist",
			packageName: "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			outputFile := filepath.Join(t.TempDir(), "assets.go")

			// when
			err := generateAssetsGo(tt.varName, tt.embedDir, tt.packageName, outputFile)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil && err == nil {
				tt.check(t, outputFile)
			}
		})
	}
}
