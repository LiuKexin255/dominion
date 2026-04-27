package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageArtifacts(t *testing.T) {
	tests := []struct {
		name          string
		artifactFiles map[string]string
		artifacts     []*Artifact
		wantFiles     map[string]string
		wantErr       bool
	}{
		{
			name: "copies and renames artifact to staging dir",
			artifactFiles: map[string]string{
				"//pkg:app": "binary content",
			},
			artifacts: []*Artifact{
				{
					Target:   "//pkg:app",
					Filename: "app-linux-amd64.zip",
					Platform: "linux",
					Arch:     "amd64",
				},
			},
			wantFiles: map[string]string{
				"app-linux-amd64.zip": "binary content",
			},
		},
		{
			name: "returns error for nonexistent label",
			artifactFiles: map[string]string{
				"//pkg:other": "binary content",
			},
			artifacts: []*Artifact{
				{
					Target:   "//pkg:missing",
					Filename: "app-linux-amd64.zip",
					Platform: "linux",
					Arch:     "amd64",
				},
			},
			wantErr: true,
		},
		{
			name: "copies multiple artifacts",
			artifactFiles: map[string]string{
				"//pkg:linux":   "linux content",
				"//pkg:windows": "windows content",
			},
			artifacts: []*Artifact{
				{
					Target:   "//pkg:linux",
					Filename: "app-linux-amd64.zip",
					Platform: "linux",
					Arch:     "amd64",
				},
				{
					Target:   "//pkg:windows",
					Filename: "app-windows-amd64.zip",
					Platform: "windows",
					Arch:     "amd64",
				},
			},
			wantFiles: map[string]string{
				"app-linux-amd64.zip":   "linux content",
				"app-windows-amd64.zip": "windows content",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stagingDir := t.TempDir()
			sourceDir := t.TempDir()
			artifactPaths := map[string]string{}
			for label, content := range tt.artifactFiles {
				path := filepath.Join(sourceDir, strings.ReplaceAll(label, "/", "_"))
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatalf("write source artifact %q: %v", path, err)
				}
				artifactPaths[label] = path
			}

			err := StageArtifacts(stagingDir, tt.artifacts, artifactPaths)

			if tt.wantErr && err == nil {
				t.Fatalf("StageArtifacts() expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("StageArtifacts() unexpected error: %v", err)
			}
			for filename, wantContent := range tt.wantFiles {
				got, err := os.ReadFile(filepath.Join(stagingDir, filename))
				if err != nil {
					t.Fatalf("read staged file %q: %v", filename, err)
				}
				if string(got) != wantContent {
					t.Fatalf("staged file %q content = %q, want %q", filename, string(got), wantContent)
				}
			}
		})
	}
}

func TestStageArtifacts_DirectoryInsteadOfFile(t *testing.T) {
	stagingDir := t.TempDir()
	sourceDir := t.TempDir()
	artifactPaths := map[string]string{
		"//pkg:app": sourceDir,
	}
	artifacts := []*Artifact{
		{
			Target:   "//pkg:app",
			Filename: "app-linux-amd64.zip",
			Platform: "linux",
			Arch:     "amd64",
		},
	}

	err := StageArtifacts(stagingDir, artifacts, artifactPaths)

	if err == nil {
		t.Fatalf("StageArtifacts() expected error for directory source")
	}
}

func Test_fileSize(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int64
	}{
		{name: "empty file", content: "", want: 0},
		{name: "non-empty file", content: "hello", want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "file.txt")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatalf("write file: %v", err)
			}

			got, err := fileSize(path)

			if err != nil {
				t.Fatalf("fileSize(%q) unexpected error: %v", path, err)
			}
			if got != tt.want {
				t.Fatalf("fileSize(%q) = %d, want %d", path, got, tt.want)
			}
		})
	}
}

func Test_fileSHA256(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "hello world",
			content: "hello world",
			want:    "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
		{
			name:    "empty",
			content: "",
			want:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "file.txt")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatalf("write file: %v", err)
			}

			got, err := fileSHA256(path)

			if err != nil {
				t.Fatalf("fileSHA256(%q) unexpected error: %v", path, err)
			}
			if got != tt.want {
				t.Fatalf("fileSHA256(%q) = %q, want %q", path, got, tt.want)
			}
		})
	}
}

func TestGenerateOutputManifest(t *testing.T) {
	stagingDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stagingDir, "app-linux-amd64.zip"), []byte("hello world"), 0644); err != nil {
		t.Fatalf("write staged artifact: %v", err)
	}
	manifest := &ReleaseManifest{
		Name:    "sample-app",
		Version: "1.2.3",
		Artifacts: []*Artifact{
			{
				Target:   "//pkg:app",
				Filename: "app-linux-amd64.zip",
				Platform: "linux",
				Arch:     "amd64",
			},
		},
	}

	got, err := GenerateOutputManifest(manifest, stagingDir)

	if err != nil {
		t.Fatalf("GenerateOutputManifest() unexpected error: %v", err)
	}
	if got.Name != "sample-app" {
		t.Fatalf("OutputManifest.Name = %q, want %q", got.Name, "sample-app")
	}
	if got.Version != "1.2.3" {
		t.Fatalf("OutputManifest.Version = %q, want %q", got.Version, "1.2.3")
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("len(OutputManifest.Artifacts) = %d, want 1", len(got.Artifacts))
	}
	artifact := got.Artifacts[0]
	if artifact.Filename != "app-linux-amd64.zip" || artifact.Platform != "linux" || artifact.Arch != "amd64" {
		t.Fatalf("OutputManifest artifact = %+v, want filename/platform/arch from manifest", artifact)
	}
	if artifact.Size != 11 {
		t.Fatalf("OutputManifest artifact size = %d, want 11", artifact.Size)
	}
	if artifact.SHA256 != "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9" {
		t.Fatalf("OutputManifest artifact sha256 = %q", artifact.SHA256)
	}

	data, err := os.ReadFile(filepath.Join(stagingDir, outputManifestFile))
	if err != nil {
		t.Fatalf("read manifest.json: %v", err)
	}
	var decoded OutputManifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal manifest.json: %v", err)
	}
	if decoded.Name != got.Name || decoded.Version != got.Version || len(decoded.Artifacts) != 1 {
		t.Fatalf("manifest.json decoded = %+v, want generated output", decoded)
	}
}

func TestGenerateSHA256SUMS(t *testing.T) {
	tests := []struct {
		name         string
		files        map[string]string
		existingSums string
		wantOrder    []string
		wantExcluded string
	}{
		{
			name: "includes artifacts and manifest",
			files: map[string]string{
				"artifact.zip":  "hello world",
				"manifest.json": "{}",
			},
			wantOrder: []string{"artifact.zip", "manifest.json"},
		},
		{
			name: "excludes SHA256SUMS itself",
			files: map[string]string{
				"artifact.zip":  "hello world",
				"manifest.json": "{}",
			},
			existingSums: "old sums",
			wantOrder:    []string{"artifact.zip", "manifest.json"},
			wantExcluded: sha256SumsFile,
		},
		{
			name: "sorts entries by filename",
			files: map[string]string{
				"z.zip":         "z",
				"manifest.json": "{}",
				"a.zip":         "a",
			},
			wantOrder: []string{"a.zip", "manifest.json", "z.zip"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stagingDir := t.TempDir()
			for filename, content := range tt.files {
				if err := os.WriteFile(filepath.Join(stagingDir, filename), []byte(content), 0644); err != nil {
					t.Fatalf("write staged file %q: %v", filename, err)
				}
			}
			if tt.existingSums != "" {
				if err := os.WriteFile(filepath.Join(stagingDir, sha256SumsFile), []byte(tt.existingSums), 0644); err != nil {
					t.Fatalf("write existing SHA256SUMS: %v", err)
				}
			}

			err := GenerateSHA256SUMS(stagingDir)

			if err != nil {
				t.Fatalf("GenerateSHA256SUMS() unexpected error: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(stagingDir, sha256SumsFile))
			if err != nil {
				t.Fatalf("read SHA256SUMS: %v", err)
			}
			lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			if len(lines) != len(tt.wantOrder) {
				t.Fatalf("SHA256SUMS line count = %d, want %d; content: %q", len(lines), len(tt.wantOrder), string(data))
			}
			for i, filename := range tt.wantOrder {
				wantHash, err := fileSHA256(filepath.Join(stagingDir, filename))
				if err != nil {
					t.Fatalf("hash staged file %q: %v", filename, err)
				}
				wantLine := wantHash + "  " + filename
				if lines[i] != wantLine {
					t.Fatalf("SHA256SUMS line %d = %q, want %q", i, lines[i], wantLine)
				}
			}
			if tt.wantExcluded != "" && strings.Contains(string(data), tt.wantExcluded) {
				t.Fatalf("SHA256SUMS content %q includes excluded file %q", string(data), tt.wantExcluded)
			}
		})
	}
}

func TestOutputManifestJSONFormat(t *testing.T) {
	manifest := &OutputManifest{
		Name:    "sample-app",
		Version: "1.2.3",
		Artifacts: []*OutputArtifact{
			{
				Filename: "app-linux-amd64.zip",
				Platform: "linux",
				Arch:     "amd64",
				Size:     11,
				SHA256:   "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
			},
		},
	}

	data, err := json.MarshalIndent(manifest, "", "  ")

	if err != nil {
		t.Fatalf("json.MarshalIndent() unexpected error: %v", err)
	}
	want := `{
  "name": "sample-app",
  "version": "1.2.3",
  "artifacts": [
    {
      "filename": "app-linux-amd64.zip",
      "platform": "linux",
      "arch": "amd64",
      "size": 11,
      "sha256": "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
    }
  ]
}`
	if string(data) != want {
		t.Fatalf("OutputManifest JSON = %s, want %s", string(data), want)
	}
}
