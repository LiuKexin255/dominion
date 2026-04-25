package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildManifest(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"windows-agent-0.1.0-portable.zip": "agent",
		"input-helper.exe":                 "helper",
		"ffmpeg.exe":                       "ffmpeg",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile(%q) unexpected error: %v", path, err)
		}
	}

	buildTime := time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC)
	got, err := BuildManifest(&ManifestOptions{
		Version:         "0.1.0",
		DistDir:         dir,
		GitCommit:       "abc123",
		BuildTime:       buildTime,
		ElectronVersion: "35.0.0",
		FFmpegVersion:   "7.1",
		FFmpegFilename:  "ffmpeg.exe",
	})

	if err != nil {
		t.Fatalf("BuildManifest() unexpected error: %v", err)
	}
	if got.Version != "0.1.0" || got.GitCommit != "abc123" || got.BuildTime != "2026-04-24T00:00:00Z" {
		t.Fatalf("BuildManifest() metadata = %#v, want version/git/build time", got)
	}
	if got.Platform != "windows" || got.Arch != "amd64" {
		t.Fatalf("BuildManifest() platform/arch = %s/%s, want windows/amd64", got.Platform, got.Arch)
	}
	if got.ElectronVersion != "35.0.0" || got.FFmpegVersion != "7.1" {
		t.Fatalf("BuildManifest() dependency versions = %s/%s, want 35.0.0/7.1", got.ElectronVersion, got.FFmpegVersion)
	}
	if got.FFmpegSHA256 != "6862fa01d6f0bc4c9601c1a0a9d170cb49cf255b25bfc66a02f958fa47be43a2" {
		t.Fatalf("BuildManifest() ffmpeg sha = %q, want checksum of ffmpeg.exe", got.FFmpegSHA256)
	}
	if len(got.Artifacts) != 3 {
		t.Fatalf("BuildManifest() artifacts len = %d, want 3", len(got.Artifacts))
	}
	if got.Artifacts[0].Filename != "ffmpeg.exe" || got.Artifacts[1].Filename != "input-helper.exe" || got.Artifacts[2].Filename != "windows-agent-0.1.0-portable.zip" {
		t.Fatalf("BuildManifest() artifacts = %#v, want sorted by filename", got.Artifacts)
	}
	if got.Artifacts[2].Size != int64(len("agent")) || got.Artifacts[2].SHA256 == "" {
		t.Fatalf("BuildManifest() artifact = %#v, want size and sha256", got.Artifacts[2])
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal(manifest) unexpected error: %v", err)
	}
	if !json.Valid(encoded) {
		t.Fatalf("Marshal(manifest) = %q, want valid JSON", encoded)
	}
}

func TestParseS3URL(t *testing.T) {
	tests := []struct {
		name       string
		rawURL     string
		wantBucket string
		wantPrefix string
		wantErr    bool
	}{
		{name: "bucket with prefix", rawURL: "s3://game-release/windows/", wantBucket: "game-release", wantPrefix: "windows/"},
		{name: "bucket only", rawURL: "s3://game-release", wantBucket: "game-release", wantPrefix: ""},
		{name: "prefix normalized", rawURL: "s3://game-release/windows", wantBucket: "game-release", wantPrefix: "windows/"},
		{name: "wrong scheme", rawURL: "https://game-release/windows", wantErr: true},
		{name: "missing bucket", rawURL: "s3:///windows", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseS3URL(tt.rawURL)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseS3URL(%q) expected error", tt.rawURL)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseS3URL(%q) unexpected error: %v", tt.rawURL, err)
			}
			if got.Bucket != tt.wantBucket || got.Prefix != tt.wantPrefix {
				t.Fatalf("ParseS3URL(%q) = %#v, want bucket %q prefix %q", tt.rawURL, got, tt.wantBucket, tt.wantPrefix)
			}
		})
	}
}
