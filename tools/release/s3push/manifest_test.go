package main

import (
	"strings"
	"testing"
)

func TestParseManifest(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		wantErr     bool
		errSubstr   string
	}{
		{
			name: "parses valid yaml manifest",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //projects/game/windows_agent:windows_agent_win_x64_zip
    filename: windows-agent-0.1.0-windows-amd64.zip
    platform: windows
    arch: amd64
`,
		},
		{
			name: "accepts version 0.1.0",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
		},
		{
			name: "accepts version 1.2.3",
			yamlContent: `name: windows-agent
version: 1.2.3
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
		},
		{
			name: "rejects version with v prefix",
			yamlContent: `name: windows-agent
version: v1.2.3
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "version",
		},
		{
			name: "rejects short version",
			yamlContent: `name: windows-agent
version: 1.2
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "version",
		},
		{
			name: "rejects prerelease version",
			yamlContent: `name: windows-agent
version: 1.2.3-beta.1
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "version",
		},
		{
			name: "accepts windows platform",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_windows_amd64
    filename: app-windows-amd64.zip
    platform: windows
    arch: amd64
`,
		},
		{
			name: "accepts linux platform",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
		},
		{
			name: "accepts darwin platform",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_darwin_amd64
    filename: app-darwin-amd64.zip
    platform: darwin
    arch: amd64
`,
		},
		{
			name: "rejects win platform",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_win_amd64
    filename: app-win-amd64.zip
    platform: win
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "platform",
		},
		{
			name: "rejects macos platform",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_macos_amd64
    filename: app-macos-amd64.zip
    platform: macos
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "platform",
		},
		{
			name: "accepts amd64 arch",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
		},
		{
			name: "accepts 386 arch",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_386
    filename: app-linux-386.zip
    platform: linux
    arch: "386"
`,
		},
		{
			name: "accepts arm64 arch",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_arm64
    filename: app-linux-arm64.zip
    platform: linux
    arch: arm64
`,
		},
		{
			name: "accepts arm arch",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_arm
    filename: app-linux-arm.zip
    platform: linux
    arch: arm
`,
		},
		{
			name: "rejects x64 arch",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_x64
    filename: app-linux-x64.zip
    platform: linux
    arch: x64
`,
			wantErr:   true,
			errSubstr: "arch",
		},
		{
			name: "rejects x86 arch",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_x86
    filename: app-linux-x86.zip
    platform: linux
    arch: x86
`,
			wantErr:   true,
			errSubstr: "arch",
		},
		{
			name: "rejects missing name",
			yamlContent: `version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "name",
		},
		{
			name: "rejects missing version",
			yamlContent: `name: windows-agent
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "version",
		},
		{
			name: "rejects empty artifacts",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts: []
`,
			wantErr:   true,
			errSubstr: "artifacts",
		},
		{
			name: "rejects missing target",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "target",
		},
		{
			name: "rejects missing filename",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    platform: linux
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "filename",
		},
		{
			name: "rejects missing platform",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "platform",
		},
		{
			name: "rejects missing arch",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
`,
			wantErr:   true,
			errSubstr: "arch",
		},
		{
			name: "rejects duplicate target",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
  - target: //pkg:app_linux_amd64
    filename: app-windows-amd64.zip
    platform: windows
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "duplicate target",
		},
		{
			name: "rejects duplicate filename",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app.zip
    platform: linux
    arch: amd64
  - target: //pkg:app_windows_amd64
    filename: app.zip
    platform: windows
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "duplicate filename",
		},
		{
			name: "rejects duplicate platform arch",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
  - target: //pkg:other_linux_amd64
    filename: other-linux-amd64.zip
    platform: linux
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "duplicate platform and arch",
		},
		{
			name: "accepts windows-agent name",
			yamlContent: `name: windows-agent
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
		},
		{
			name: "accepts my-app name",
			yamlContent: `name: my-app
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
		},
		{
			name: "rejects uppercase name",
			yamlContent: `name: MyApp
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "name",
		},
		{
			name: "rejects underscore name",
			yamlContent: `name: _invalid
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "name",
		},
		{
			name: "rejects leading hyphen name",
			yamlContent: `name: -leading
version: 0.1.0
artifacts:
  - target: //pkg:app_linux_amd64
    filename: app-linux-amd64.zip
    platform: linux
    arch: amd64
`,
			wantErr:   true,
			errSubstr: "name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			data := []byte(tt.yamlContent)

			// when
			manifest, err := ParseManifest(data)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseManifest() expected error")
				}
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("ParseManifest() error = %v, want substring %q", err, tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseManifest() unexpected error: %v", err)
			}
			if manifest == nil {
				t.Fatal("ParseManifest() manifest is nil")
			}
		})
	}
}

func TestValidateTargets(t *testing.T) {
	tests := []struct {
		name            string
		manifestTargets []string
		bazelTargets    []string
		wantErr         bool
		errSubstr       string
	}{
		{
			name:            "accepts exact match",
			manifestTargets: []string{"//pkg:linux", "//pkg:windows"},
			bazelTargets:    []string{"//pkg:windows", "//pkg:linux"},
		},
		{
			name:            "rejects manifest extra target",
			manifestTargets: []string{"//pkg:linux", "//pkg:windows"},
			bazelTargets:    []string{"//pkg:linux"},
			wantErr:         true,
			errSubstr:       "do not match",
		},
		{
			name:            "rejects bazel extra target",
			manifestTargets: []string{"//pkg:linux"},
			bazelTargets:    []string{"//pkg:linux", "//pkg:windows"},
			wantErr:         true,
			errSubstr:       "do not match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := ValidateTargets(tt.manifestTargets, tt.bazelTargets)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateTargets() expected error")
				}
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("ValidateTargets() error = %v, want substring %q", err, tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateTargets() unexpected error: %v", err)
			}
		})
	}
}
