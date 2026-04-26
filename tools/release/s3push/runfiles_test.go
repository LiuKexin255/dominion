package main

import (
	"os"
	"testing"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "success with single artifact",
			args:    []string{"--manifest=/path/to/manifest.yaml", "--artifact=//pkg:foo,/path/to/foo.zip"},
			wantErr: false,
		},
		{
			name: "success with multiple artifacts",
			args: []string{
				"--manifest=/path/to/manifest.yaml",
				"--artifact=//pkg:foo,/path/to/foo.zip",
				"--artifact=//pkg:bar,/path/to/bar.zip",
			},
			wantErr: false,
		},
		{
			name:    "missing --manifest",
			args:    []string{"--artifact=//pkg:foo,/path/to/foo.zip"},
			wantErr: true,
		},
		{
			name:    "missing --artifact",
			args:    []string{"--manifest=/path/to/manifest.yaml"},
			wantErr: true,
		},
		{
			name:    "artifact label empty",
			args:    []string{"--manifest=/path/to/manifest.yaml", "--artifact=,/path/to/foo.zip"},
			wantErr: true,
		},
		{
			name:    "artifact filepath empty",
			args:    []string{"--manifest=/path/to/manifest.yaml", "--artifact=//pkg:foo,"},
			wantErr: true,
		},
		{
			name: "duplicate artifact labels",
			args: []string{
				"--manifest=/path/to/manifest.yaml",
				"--artifact=//pkg:foo,/path/to/foo.zip",
				"--artifact=//pkg:foo,/path/to/bar.zip",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			_, err := ParseArgs(tt.args)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("ParseArgs(%v) expected error", tt.args)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ParseArgs(%v) unexpected error: %v", tt.args, err)
			}
		})
	}
}

func TestParseArgs_multipleArtifactsParsed(t *testing.T) {
	// given
	args := []string{
		"--manifest=/path/to/manifest.yaml",
		"--artifact=//pkg:first,/path/to/first.zip",
		"--artifact=//pkg:second,/path/to/second.zip",
		"--artifact=//pkg:third,/path/to/third.zip",
	}

	// when
	config, err := ParseArgs(args)
	if err != nil {
		t.Fatalf("ParseArgs(%v) unexpected error: %v", args, err)
	}

	// then
	if config.ManifestPath != "/path/to/manifest.yaml" {
		t.Fatalf("ManifestPath = %q, want %q", config.ManifestPath, "/path/to/manifest.yaml")
	}
	if len(config.Artifacts) != 3 {
		t.Fatalf("len(Artifacts) = %d, want 3", len(config.Artifacts))
	}
	if config.Artifacts[0].Label != "//pkg:first" {
		t.Fatalf("Artifacts[0].Label = %q, want %q", config.Artifacts[0].Label, "//pkg:first")
	}
	if config.Artifacts[0].FilePath != "/path/to/first.zip" {
		t.Fatalf("Artifacts[0].FilePath = %q, want %q", config.Artifacts[0].FilePath, "/path/to/first.zip")
	}
	if config.Artifacts[1].Label != "//pkg:second" {
		t.Fatalf("Artifacts[1].Label = %q, want %q", config.Artifacts[1].Label, "//pkg:second")
	}
	if config.Artifacts[1].FilePath != "/path/to/second.zip" {
		t.Fatalf("Artifacts[1].FilePath = %q, want %q", config.Artifacts[1].FilePath, "/path/to/second.zip")
	}
	if config.Artifacts[2].Label != "//pkg:third" {
		t.Fatalf("Artifacts[2].Label = %q, want %q", config.Artifacts[2].Label, "//pkg:third")
	}
	if config.Artifacts[2].FilePath != "/path/to/third.zip" {
		t.Fatalf("Artifacts[2].FilePath = %q, want %q", config.Artifacts[2].FilePath, "/path/to/third.zip")
	}
}

func TestResolveArtifacts(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) *RunConfig
		wantErr bool
	}{
		{
			name: "success resolves label to filepath",
			setup: func(t *testing.T) *RunConfig {
				dir := t.TempDir()
				file1 := dir + "/foo.zip"
				file2 := dir + "/bar.zip"
				if err := os.WriteFile(file1, []byte("foo"), 0644); err != nil {
					t.Fatalf("WriteFile error: %v", err)
				}
				if err := os.WriteFile(file2, []byte("bar"), 0644); err != nil {
					t.Fatalf("WriteFile error: %v", err)
				}
				return &RunConfig{
					ManifestPath: dir + "/manifest.yaml",
					Artifacts: []*ArtifactMapping{
						{Label: "//pkg:foo", FilePath: file1},
						{Label: "//pkg:bar", FilePath: file2},
					},
				}
			},
			wantErr: false,
		},
		{
			name: "nonexistent file returns error",
			setup: func(t *testing.T) *RunConfig {
				return &RunConfig{
					ManifestPath: "/nonexistent/manifest.yaml",
					Artifacts: []*ArtifactMapping{
						{Label: "//pkg:missing", FilePath: "/nonexistent/file.zip"},
					},
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			config := tt.setup(t)

			// when
			result, err := ResolveArtifacts(config)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatal("ResolveArtifacts() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveArtifacts() unexpected error: %v", err)
			}
			if len(result) != len(config.Artifacts) {
				t.Fatalf("len(result) = %d, want %d", len(result), len(config.Artifacts))
			}
			for _, a := range config.Artifacts {
				got, ok := result[a.Label]
				if !ok {
					t.Fatalf("result missing key %q", a.Label)
				}
				if got != a.FilePath {
					t.Fatalf("result[%q] = %q, want %q", a.Label, got, a.FilePath)
				}
			}
		})
	}
}
