package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/tc-hib/winres"
)

func TestFlagParsing(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantIcon string
		wantOut  string
		wantMan  string
		wantInfo string
	}{
		{
			name:     "missing required flags",
			args:     []string{"generate_winres", "-out", "/tmp/test.syso"},
			wantIcon: "",
			wantOut:  "/tmp/test.syso",
		},
		{
			name:     "only required flags",
			args:     []string{"generate_winres", "-icon", "/path/icon.ico", "-out", "/path/out.syso"},
			wantIcon: "/path/icon.ico",
			wantOut:  "/path/out.syso",
		},
		{
			name:     "all flags",
			args:     []string{"generate_winres", "-icon", "/path/to/icon.ico", "-out", "/path/to/output.syso", "-manifest", "/path/to/manifest.xml", "-info", "/path/to/info.json"},
			wantIcon: "/path/to/icon.ico",
			wantOut:  "/path/to/output.syso",
			wantMan:  "/path/to/manifest.xml",
			wantInfo: "/path/to/info.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldArgs := os.Args
			defer func() { os.Args = oldArgs }()

			os.Args = tt.args
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

			icon := flag.String("icon", "", "")
			out := flag.String("out", "", "")
			manifest := flag.String("manifest", "", "")
			info := flag.String("info", "", "")
			flag.Parse()

			if *icon != tt.wantIcon {
				t.Errorf("icon = %q, want %q", *icon, tt.wantIcon)
			}
			if *out != tt.wantOut {
				t.Errorf("out = %q, want %q", *out, tt.wantOut)
			}
			if *manifest != tt.wantMan {
				t.Errorf("manifest = %q, want %q", *manifest, tt.wantMan)
			}
			if *info != tt.wantInfo {
				t.Errorf("info = %q, want %q", *info, tt.wantInfo)
			}
		})
	}
}

func TestIconFileCheck(t *testing.T) {
	tmpDir := t.TempDir()
	iconPath := filepath.Join(tmpDir, "test.ico")
	if err := os.WriteFile(iconPath, []byte{0, 0, 1, 0, 1, 0}, 0644); err != nil {
		t.Fatalf("failed to create test icon: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"existing icon", iconPath, false},
		{"non-existent icon", filepath.Join(tmpDir, "nonexistent.ico"), true},
		{"empty path", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := os.Stat(tt.path)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestArchFromString(t *testing.T) {
	tests := []struct {
		input string
		want  winres.Arch
		err   bool
	}{
		{"amd64", winres.ArchAMD64, false},
		{"x86_64", winres.ArchAMD64, false},
		{"386", winres.ArchI386, false},
		{"i386", winres.ArchI386, false},
		{"x86", winres.ArchI386, false},
		{"arm64", winres.ArchARM64, false},
		{"aarch64", winres.ArchARM64, false},
		{"unknown", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := archFromString(tt.input)
			if tt.err {
				if err == nil {
					t.Errorf("archFromString(%q) expected error, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("archFromString(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("archFromString(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateValidation_MissingIcon(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"generate_winres", "-out", "/tmp/test.syso"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	err := generate()
	if err == nil {
		t.Error("expected error for missing -icon flag, got nil")
	}
}

func TestGenerateValidation_MissingOut(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"generate_winres", "-icon", "/tmp/test.ico"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	err := generate()
	if err == nil {
		t.Error("expected error for missing -out flag, got nil")
	}
}

func TestGenerateIconNonExistent(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"generate_winres", "-icon", "/nonexistent/icon.ico", "-out", "/tmp/out.syso"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	err := generate()
	if err == nil {
		t.Fatal("expected error for non-existent icon, got nil")
	}
}
