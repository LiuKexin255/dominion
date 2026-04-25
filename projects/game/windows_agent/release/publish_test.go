package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestRunDryRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "input-helper.exe"), []byte("helper"), 0644); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}

	originalStdout := stdout
	originalNewS3Client := newS3Client
	var out bytes.Buffer
	stdout = &out
	newS3Client = func(region string) (*minio.Client, error) {
		t.Fatalf("dry-run must not create S3 client")
		return nil, nil
	}
	t.Cleanup(func() {
		stdout = originalStdout
		newS3Client = originalNewS3Client
	})

	err := run([]string{
		"--version=0.1.0",
		"--s3-url=s3://game-release/windows/",
		"--dist-dir=" + dir,
		"--dry-run",
	})

	if err != nil {
		t.Fatalf("run(dry-run) unexpected error: %v", err)
	}
	got := out.String()
	for _, want := range []string{"DRY RUN", "manifest.json", "SHA256SUMS", "input-helper.exe", "s3://game-release/windows/"} {
		if !strings.Contains(got, want) {
			t.Fatalf("run(dry-run) output = %q, want substring %q", got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ManifestFilename)); err != nil {
		t.Fatalf("run(dry-run) manifest file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, SHA256SumsFilename)); err != nil {
		t.Fatalf("run(dry-run) SHA256SUMS file missing: %v", err)
	}
}

func TestUploadFiles(t *testing.T) {
	tests := []struct {
		name       string
		dest       *S3Destination
		files      []string
		wantObject []string
	}{
		{
			name:       "prefix upload",
			dest:       &S3Destination{Bucket: "game-release", Prefix: "windows/"},
			files:      []string{"manifest.json", "SHA256SUMS"},
			wantObject: []string{"windows/manifest.json", "windows/SHA256SUMS"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, file := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0644); err != nil {
					t.Fatalf("WriteFile(%q) unexpected error: %v", file, err)
				}
			}
			var gotObjects []string
			uploader := func(ctx context.Context, client *minio.Client, bucket string, object string, path string, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
				if bucket != tt.dest.Bucket {
					t.Fatalf("bucket = %q, want %q", bucket, tt.dest.Bucket)
				}
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("upload path %q missing: %v", path, err)
				}
				gotObjects = append(gotObjects, object)
				return minio.UploadInfo{}, nil
			}

			err := uploadFiles(context.Background(), new(minio.Client), tt.dest, dir, tt.files, uploader)

			if err != nil {
				t.Fatalf("uploadFiles() unexpected error: %v", err)
			}
			if strings.Join(gotObjects, ",") != strings.Join(tt.wantObject, ",") {
				t.Fatalf("uploadFiles() objects = %v, want %v", gotObjects, tt.wantObject)
			}
		})
	}
}

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "all flags", args: []string{"--version=0.1.0", "--s3-url=s3://bucket/prefix/", "--dist-dir=/tmp/dist"}},
		{name: "dry run", args: []string{"--version=0.1.0", "--s3-url=s3://bucket/prefix/", "--dist-dir=/tmp/dist", "--dry-run"}},
		{name: "missing version", args: []string{"--s3-url=s3://bucket/prefix/", "--dist-dir=/tmp/dist"}, wantErr: true},
		{name: "missing s3 url", args: []string{"--version=0.1.0", "--dist-dir=/tmp/dist"}, wantErr: true},
		{name: "missing dist dir", args: []string{"--version=0.1.0", "--s3-url=s3://bucket/prefix/"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOptions(tt.args, time.Now)

			if tt.wantErr && err == nil {
				t.Fatalf("parseOptions(%v) expected error", tt.args)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("parseOptions(%v) unexpected error: %v", tt.args, err)
			}
		})
	}
}
