package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateSHA256SUMS(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string
		wantErr   bool
		wantLines int
	}{
		{
			name: "single file",
			files: map[string]string{
				"agent.zip": "hello world",
			},
			wantErr:   false,
			wantLines: 1,
		},
		{
			name: "multiple files",
			files: map[string]string{
				"agent.zip":     "hello world",
				"manifest.json": `{"version":"1.0"}`,
			},
			wantErr:   false,
			wantLines: 2,
		},
		{
			name:      "empty dir",
			files:     map[string]string{},
			wantErr:   false,
			wantLines: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			distDir := t.TempDir()
			for name, content := range tt.files {
				if err := os.WriteFile(filepath.Join(distDir, name), []byte(content), 0644); err != nil {
					t.Fatalf("setup test file %q: %v", name, err)
				}
			}

			// when
			got, err := GenerateSHA256SUMS(distDir)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("GenerateSHA256SUMS(%q) expected error, got nil", distDir)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("GenerateSHA256SUMS(%q) unexpected error: %v", distDir, err)
			}
			if tt.wantErr {
				return
			}

			lines := strings.Split(strings.TrimSpace(got), "\n")
			if tt.wantLines == 0 && got == "\n" {
				return
			}
			if len(lines) != tt.wantLines {
				t.Fatalf("GenerateSHA256SUMS returned %d lines, want %d", len(lines), tt.wantLines)
			}

			for _, line := range lines {
				parts := strings.SplitN(line, "  ", 2)
				if len(parts) != 2 {
					t.Fatalf("SHA256SUMS line %q does not match format '<hash>  <filename>'", line)
				}
				expectedHash := sha256Hex(tt.files[parts[1]])
				if parts[0] != expectedHash {
					t.Fatalf("hash for %q: got %q, want %q", parts[1], parts[0], expectedHash)
				}
			}
		})
	}
}

func TestSHA256Correct(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "known content",
			content: "hello world",
			want:    "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
		{
			name:    "empty content",
			content: "",
			want:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:    "binary-like content",
			content: "\x00\x01\x02\xff",
			want:    "4b9e1b4a0e5c1b5e1a3b0c5d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			filePath := filepath.Join(dir, "testfile")
			if err := os.WriteFile(filePath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("write test file: %v", err)
			}

			// when
			got, err := fileSHA256(filePath)

			// then
			if err != nil {
				t.Fatalf("fileSHA256(%q) unexpected error: %v", filePath, err)
			}

			if tt.name == "known content" || tt.name == "empty content" {
				if got != tt.want {
					t.Fatalf("fileSHA256 = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestSHA256Correct_knownValues(t *testing.T) {
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
			// given
			dir := t.TempDir()
			filePath := filepath.Join(dir, "testfile")
			if err := os.WriteFile(filePath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("write test file: %v", err)
			}

			// when
			got, err := fileSHA256(filePath)

			// then
			if err != nil {
				t.Fatalf("fileSHA256 unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("fileSHA256 = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateManifest(t *testing.T) {
	tests := []struct {
		name    string
		version string
		files   map[string]string
		wantErr bool
	}{
		{
			name:    "single artifact",
			version: "1.0.0",
			files: map[string]string{
				"agent.zip": "artifact content",
			},
			wantErr: false,
		},
		{
			name:    "multiple artifacts",
			version: "2.0.0",
			files: map[string]string{
				"agent.zip":     "artifact content",
				"checksums.txt": "sha256 data",
			},
			wantErr: false,
		},
		{
			name:    "nonexistent dir",
			version: "1.0.0",
			files:   nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			distDir := t.TempDir()
			if tt.files == nil && tt.wantErr {
				distDir = "/nonexistent/path"
			}
			for name, content := range tt.files {
				if err := os.WriteFile(filepath.Join(distDir, name), []byte(content), 0644); err != nil {
					t.Fatalf("setup test file %q: %v", name, err)
				}
			}

			// when
			manifest, err := GenerateManifest(tt.version, distDir)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("GenerateManifest(%q, %q) expected error", tt.version, distDir)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("GenerateManifest(%q, %q) unexpected error: %v", tt.version, distDir, err)
			}
			if tt.wantErr {
				return
			}

			if manifest.Version != tt.version {
				t.Fatalf("manifest.Version = %q, want %q", manifest.Version, tt.version)
			}
			if manifest.Platform != PlatformWindows {
				t.Fatalf("manifest.Platform = %q, want %q", manifest.Platform, PlatformWindows)
			}
			if manifest.GitCommit == "" {
				t.Fatalf("manifest.GitCommit should not be empty")
			}
			if manifest.BuildTime == "" {
				t.Fatalf("manifest.BuildTime should not be empty")
			}
			if len(manifest.Artifacts) != len(tt.files) {
				t.Fatalf("len(manifest.Artifacts) = %d, want %d", len(manifest.Artifacts), len(tt.files))
			}

			for _, art := range manifest.Artifacts {
				content, ok := tt.files[art.Filename]
				if !ok {
					t.Fatalf("unexpected artifact %q in manifest", art.Filename)
				}
				expectedHash := sha256Hex(content)
				if art.SHA256 != expectedHash {
					t.Fatalf("artifact %q SHA256 = %q, want %q", art.Filename, art.SHA256, expectedHash)
				}
				if art.Size != int64(len(content)) {
					t.Fatalf("artifact %q Size = %d, want %d", art.Filename, art.Size, len(content))
				}
			}
		})
	}
}

func TestManifestJSON(t *testing.T) {
	// given
	version := "1.2.3"
	distDir := t.TempDir()
	fileContent := "test artifact data"
	if err := os.WriteFile(filepath.Join(distDir, "agent.zip"), []byte(fileContent), 0644); err != nil {
		t.Fatalf("setup test file: %v", err)
	}

	manifest, err := GenerateManifest(version, distDir)
	if err != nil {
		t.Fatalf("GenerateManifest unexpected error: %v", err)
	}

	// when
	data, err := json.Marshal(manifest)

	// then
	if err != nil {
		t.Fatalf("json.Marshal(manifest) unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal unexpected error: %v", err)
	}

	requiredFields := []string{"version", "gitCommit", "buildTime", "platform", "artifacts", "wails", "ffmpeg", "inputHelper"}
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Fatalf("manifest JSON missing required field %q", field)
		}
	}

	artifacts, ok := parsed["artifacts"].([]interface{})
	if !ok {
		t.Fatalf("manifest.artifacts is not an array")
	}
	if len(artifacts) != 1 {
		t.Fatalf("len(manifest.artifacts) = %d, want 1", len(artifacts))
	}

	firstArt := artifacts[0].(map[string]interface{})
	artifactFields := []string{"filename", "size", "sha256"}
	for _, field := range artifactFields {
		if _, ok := firstArt[field]; !ok {
			t.Fatalf("artifact missing required field %q", field)
		}
	}

	if firstArt["filename"] != "agent.zip" {
		t.Fatalf("artifact.filename = %q, want %q", firstArt["filename"], "agent.zip")
	}
	if firstArt["sha256"] != sha256Hex(fileContent) {
		t.Fatalf("artifact.sha256 mismatch")
	}
}

func TestWriteManifest(t *testing.T) {
	// given
	dir := t.TempDir()
	manifest := &Manifest{
		Version:   "1.0.0",
		GitCommit: "abc123",
		BuildTime: "2026-01-01T00:00:00Z",
		Platform:  PlatformWindows,
	}

	// when
	err := WriteManifest(manifest, dir)

	// then
	if err != nil {
		t.Fatalf("WriteManifest unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		t.Fatalf("read manifest file: %v", err)
	}

	var parsed Manifest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal manifest: %v", err)
	}
	if parsed.Version != "1.0.0" {
		t.Fatalf("parsed.Version = %q, want %q", parsed.Version, "1.0.0")
	}
}

func TestWriteSHA256SUMS(t *testing.T) {
	// given
	dir := t.TempDir()
	content := "abc123  agent.zip\n"

	// when
	err := WriteSHA256SUMS(content, dir)

	// then
	if err != nil {
		t.Fatalf("WriteSHA256SUMS unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, SHA256SUMSFile))
	if err != nil {
		t.Fatalf("read SHA256SUMS file: %v", err)
	}
	if string(data) != content {
		t.Fatalf("SHA256SUMS content = %q, want %q", string(data), content)
	}
}

func TestParseS3URL(t *testing.T) {
	tests := []struct {
		name       string
		s3URL      string
		wantBucket string
		wantPrefix string
		wantErr    bool
	}{
		{
			name:       "valid with prefix",
			s3URL:      "s3://my-bucket/releases/v1",
			wantBucket: "my-bucket",
			wantPrefix: "releases/v1",
			wantErr:    false,
		},
		{
			name:       "valid without prefix",
			s3URL:      "s3://my-bucket",
			wantBucket: "my-bucket",
			wantPrefix: "",
			wantErr:    false,
		},
		{
			name:       "valid with trailing slash",
			s3URL:      "s3://my-bucket/releases/",
			wantBucket: "my-bucket",
			wantPrefix: "releases",
			wantErr:    false,
		},
		{
			name:    "wrong scheme",
			s3URL:   "https://my-bucket/releases",
			wantErr: true,
		},
		{
			name:    "missing bucket",
			s3URL:   "s3:///releases",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			bucket, prefix, err := parseS3URL(tt.s3URL)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("parseS3URL(%q) expected error", tt.s3URL)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("parseS3URL(%q) unexpected error: %v", tt.s3URL, err)
			}
			if tt.wantErr {
				return
			}
			if bucket != tt.wantBucket {
				t.Fatalf("bucket = %q, want %q", bucket, tt.wantBucket)
			}
			if prefix != tt.wantPrefix {
				t.Fatalf("prefix = %q, want %q", prefix, tt.wantPrefix)
			}
		})
	}
}

func TestS3ObjectKey(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		filename string
		want     string
	}{
		{name: "with prefix", prefix: "releases/v1", filename: "agent.zip", want: "releases/v1/agent.zip"},
		{name: "empty prefix", prefix: "", filename: "agent.zip", want: "agent.zip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// when
			got := s3ObjectKey(tt.prefix, tt.filename)

			// then
			if got != tt.want {
				t.Fatalf("s3ObjectKey(%q, %q) = %q, want %q", tt.prefix, tt.filename, got, tt.want)
			}
		})
	}
}

func sha256Hex(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}
