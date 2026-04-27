package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uploadedFile records a single upload call made by fakeUploader.
type uploadedFile struct {
	Bucket  string
	Key     string
	Content string
	Size    int64
}

// fakeUploader is a test double for Uploader that records all calls.
type fakeUploader struct {
	uploaded []uploadedFile
	err      error
	errAfter int
}

func (f *fakeUploader) Upload(_ context.Context, bucket, objectKey string, reader io.Reader, objectSize int64) error {
	if f.err != nil && (f.errAfter < 0 || len(f.uploaded) >= f.errAfter) {
		return f.err
	}

	data, _ := io.ReadAll(reader)
	f.uploaded = append(f.uploaded, uploadedFile{
		Bucket:  bucket,
		Key:     objectKey,
		Content: string(data),
		Size:    objectSize,
	})
	return nil
}

func TestObjectKey(t *testing.T) {
	tests := []struct {
		name   string
		inName string
		inVer  string
		inFile string
		want   string
	}{
		{
			name:   "standard path",
			inName: "windows-agent",
			inVer:  "0.1.0",
			inFile: "agent.zip",
			want:   "windows-agent/0.1.0/agent.zip",
		},
		{
			name:   "different name",
			inName: "my-app",
			inVer:  "1.2.3",
			inFile: "binary",
			want:   "my-app/1.2.3/binary",
		},
		{
			name:   "different version",
			inName: "tool",
			inVer:  "10.0.0",
			inFile: "tool.exe",
			want:   "tool/10.0.0/tool.exe",
		},
		{
			name:   "different filename",
			inName: "release",
			inVer:  "2.0.1",
			inFile: "release.tar.gz",
			want:   "release/2.0.1/release.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given — tt fields describe the inputs

			// when
			got := ObjectKey(tt.inName, tt.inVer, tt.inFile)

			// then
			if got != tt.want {
				t.Fatalf("ObjectKey(%q, %q, %q) = %q, want %q", tt.inName, tt.inVer, tt.inFile, got, tt.want)
			}
		})
	}
}

func TestS3Bucket(t *testing.T) {
	// given — s3Bucket is a package-level constant

	// when
	got := s3Bucket

	// then
	want := "artifacts"
	if got != want {
		t.Fatalf("s3Bucket = %q, want %q", got, want)
	}
}

func TestPublish(t *testing.T) {
	tests := []struct {
		name       string
		manifest   *ReleaseManifest
		setupDir   func(t *testing.T, dir string)
		fakeErr    error
		fakeErrAt  int
		wantErr    bool
		errSubstr  string
		wantKeys   []string
		wantBucket string
	}{
		{
			name: "single artifact uploads in correct order",
			manifest: &ReleaseManifest{
				Name:    "my-app",
				Version: "1.0.0",
				Artifacts: []*Artifact{
					{Target: "//pkg:app", Filename: "app.zip", Platform: "linux", Arch: "amd64"},
				},
			},
			setupDir: func(t *testing.T, dir string) {
				t.Helper()
				mustWriteFile(t, filepath.Join(dir, "app.zip"), "artifact content")
				mustWriteFile(t, filepath.Join(dir, outputManifestFile), `{"name":"my-app"}`)
				mustWriteFile(t, filepath.Join(dir, sha256SumsFile), "hash  app.zip")
			},
			wantKeys:   []string{"my-app/1.0.0/app.zip", "my-app/1.0.0/manifest.json", "my-app/1.0.0/SHA256SUMS"},
			wantBucket: "artifacts",
		},
		{
			name: "multiple artifacts uploads in correct order",
			manifest: &ReleaseManifest{
				Name:    "tool",
				Version: "2.5.0",
				Artifacts: []*Artifact{
					{Target: "//pkg:linux", Filename: "tool-linux", Platform: "linux", Arch: "amd64"},
					{Target: "//pkg:windows", Filename: "tool.exe", Platform: "windows", Arch: "amd64"},
				},
			},
			setupDir: func(t *testing.T, dir string) {
				t.Helper()
				mustWriteFile(t, filepath.Join(dir, "tool-linux"), "linux binary")
				mustWriteFile(t, filepath.Join(dir, "tool.exe"), "windows binary")
				mustWriteFile(t, filepath.Join(dir, outputManifestFile), `{"name":"tool"}`)
				mustWriteFile(t, filepath.Join(dir, sha256SumsFile), "hash  tool")
			},
			wantKeys:   []string{"tool/2.5.0/tool-linux", "tool/2.5.0/tool.exe", "tool/2.5.0/manifest.json", "tool/2.5.0/SHA256SUMS"},
			wantBucket: "artifacts",
		},
		{
			name: "upload failure returns error with object key",
			manifest: &ReleaseManifest{
				Name:    "fail-app",
				Version: "0.1.0",
				Artifacts: []*Artifact{
					{Target: "//pkg:app", Filename: "app.bin", Platform: "linux", Arch: "amd64"},
				},
			},
			setupDir: func(t *testing.T, dir string) {
				t.Helper()
				mustWriteFile(t, filepath.Join(dir, "app.bin"), "data")
				mustWriteFile(t, filepath.Join(dir, outputManifestFile), "{}")
				mustWriteFile(t, filepath.Join(dir, sha256SumsFile), "hash")
			},
			fakeErr:   errors.New("network error"),
			fakeErrAt: 0,
			wantErr:   true,
			errSubstr: "fail-app/0.1.0/app.bin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			tt.setupDir(t, dir)
			uploader := &fakeUploader{
				err:      tt.fakeErr,
				errAfter: tt.fakeErrAt,
			}

			// when
			err := Publish(context.Background(), dir, tt.manifest, uploader)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Publish() expected error")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("Publish() error = %v, want substring %q", err, tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Publish() unexpected error: %v", err)
			}

			if len(uploader.uploaded) != len(tt.wantKeys) {
				t.Fatalf("Publish() uploaded %d files, want %d", len(uploader.uploaded), len(tt.wantKeys))
			}
			for i, want := range tt.wantKeys {
				got := uploader.uploaded[i].Key
				if got != want {
					t.Fatalf("Publish() upload[%d].Key = %q, want %q", i, got, want)
				}
			}
			if tt.wantBucket != "" {
				for i, f := range uploader.uploaded {
					if f.Bucket != tt.wantBucket {
						t.Fatalf("Publish() upload[%d].Bucket = %q, want %q", i, f.Bucket, tt.wantBucket)
					}
				}
			}
		})
	}
}

func Test_listStagingFiles(t *testing.T) {
	tests := []struct {
		name          string
		setupDir      func(t *testing.T, dir string)
		wantArtifacts []string
		wantManifest  string
		wantSha256    string
		wantErr       bool
	}{
		{
			name: "separates files correctly",
			setupDir: func(t *testing.T, dir string) {
				t.Helper()
				mustWriteFile(t, filepath.Join(dir, "app.zip"), "data")
				mustWriteFile(t, filepath.Join(dir, "bin.tar"), "binary")
				mustWriteFile(t, filepath.Join(dir, outputManifestFile), "{}")
				mustWriteFile(t, filepath.Join(dir, sha256SumsFile), "hash")
			},
			wantArtifacts: []string{"app.zip", "bin.tar"},
			wantManifest:  outputManifestFile,
			wantSha256:    sha256SumsFile,
		},
		{
			name: "only manifest and sha256",
			setupDir: func(t *testing.T, dir string) {
				t.Helper()
				mustWriteFile(t, filepath.Join(dir, outputManifestFile), "{}")
				mustWriteFile(t, filepath.Join(dir, sha256SumsFile), "hash")
			},
			wantArtifacts: nil,
			wantManifest:  outputManifestFile,
			wantSha256:    sha256SumsFile,
		},
		{
			name: "nonexistent directory returns error",
			setupDir: func(t *testing.T, dir string) {
				t.Helper()
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			searchDir := dir
			if tt.wantErr {
				searchDir = filepath.Join(dir, "nonexistent")
			}
			tt.setupDir(t, dir)

			// when
			artifacts, manifestFile, sha256File, err := listStagingFiles(searchDir)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("listStagingFiles() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("listStagingFiles() unexpected error: %v", err)
			}
			if manifestFile != tt.wantManifest {
				t.Fatalf("listStagingFiles() manifest = %q, want %q", manifestFile, tt.wantManifest)
			}
			if sha256File != tt.wantSha256 {
				t.Fatalf("listStagingFiles() sha256 = %q, want %q", sha256File, tt.wantSha256)
			}
			if len(artifacts) != len(tt.wantArtifacts) {
				t.Fatalf("listStagingFiles() artifacts = %v, want %v", artifacts, tt.wantArtifacts)
			}
		})
	}
}

func TestNewS3Uploader(t *testing.T) {
	// given — verify that NewS3Uploader delegates to pkg/s3.NewS3Client("")
	// Since credentials are not set in test environment, this should return an error.

	// when
	_, err := NewS3Uploader()

	// then — must error because S3_ACCESS_KEY is not set in test env
	if err == nil {
		t.Fatal("NewS3Uploader() expected error when S3 credentials are not configured")
	}
	if !strings.Contains(err.Error(), "S3_ACCESS_KEY") {
		t.Fatalf("NewS3Uploader() error = %v, want substring %q", err, "S3_ACCESS_KEY")
	}
}

func TestS3Uploader_Upload(t *testing.T) {
	// given — verify S3Uploader implements Uploader interface at compile time
	var _ Uploader = (*S3Uploader)(nil)
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file %q: %v", path, err)
	}
}

func TestPublish_uploadContent(t *testing.T) {
	// given
	dir := t.TempDir()
	content := "hello world"
	mustWriteFile(t, filepath.Join(dir, "app.bin"), content)
	mustWriteFile(t, filepath.Join(dir, outputManifestFile), "{}")
	mustWriteFile(t, filepath.Join(dir, sha256SumsFile), "hash")

	manifest := &ReleaseManifest{
		Name:    "test-app",
		Version: "3.0.0",
		Artifacts: []*Artifact{
			{Target: "//pkg:app", Filename: "app.bin", Platform: "linux", Arch: "amd64"},
		},
	}
	uploader := &fakeUploader{}

	// when
	err := Publish(context.Background(), dir, manifest, uploader)

	// then
	if err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}
	if len(uploader.uploaded) != 3 {
		t.Fatalf("Publish() uploaded %d files, want 3", len(uploader.uploaded))
	}

	// Verify artifact content and size are preserved
	got := uploader.uploaded[0]
	if got.Content != content {
		t.Fatalf("Publish() artifact content = %q, want %q", got.Content, content)
	}
	if got.Size != int64(len(content)) {
		t.Fatalf("Publish() artifact size = %d, want %d", got.Size, len(content))
	}
}

func TestPublish_manifestSha256Last(t *testing.T) {
	// given — verify that manifest.json and SHA256SUMS are always the last two uploads
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "alpha.bin"), "a")
	mustWriteFile(t, filepath.Join(dir, "beta.bin"), "b")
	mustWriteFile(t, filepath.Join(dir, outputManifestFile), "{}")
	mustWriteFile(t, filepath.Join(dir, sha256SumsFile), "hash")

	manifest := &ReleaseManifest{
		Name:    "ordered",
		Version: "1.0.0",
		Artifacts: []*Artifact{
			{Target: "//pkg:alpha", Filename: "alpha.bin", Platform: "linux", Arch: "amd64"},
			{Target: "//pkg:beta", Filename: "beta.bin", Platform: "windows", Arch: "amd64"},
		},
	}
	uploader := &fakeUploader{}

	// when
	err := Publish(context.Background(), dir, manifest, uploader)

	// then
	if err != nil {
		t.Fatalf("Publish() unexpected error: %v", err)
	}

	n := len(uploader.uploaded)
	lastKey := uploader.uploaded[n-1].Key
	secondLastKey := uploader.uploaded[n-2].Key

	wantSecondLast := "ordered/1.0.0/" + outputManifestFile
	wantLast := "ordered/1.0.0/" + sha256SumsFile

	if secondLastKey != wantSecondLast {
		t.Fatalf("second last upload key = %q, want %q", secondLastKey, wantSecondLast)
	}
	if lastKey != wantLast {
		t.Fatalf("last upload key = %q, want %q", lastKey, wantLast)
	}
}

func TestPublish_errorIncludesObjectKey(t *testing.T) {
	// given — verify upload error message contains the full S3 object key
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "app.zip"), "data")
	mustWriteFile(t, filepath.Join(dir, outputManifestFile), "{}")
	mustWriteFile(t, filepath.Join(dir, sha256SumsFile), "h")

	manifest := &ReleaseManifest{
		Name:    "err-app",
		Version: "9.9.9",
		Artifacts: []*Artifact{
			{Target: "//pkg:app", Filename: "app.zip", Platform: "linux", Arch: "amd64"},
		},
	}
	uploader := &fakeUploader{
		err:      fmt.Errorf("connection refused"),
		errAfter: 0,
	}

	// when
	err := Publish(context.Background(), dir, manifest, uploader)

	// then
	if err == nil {
		t.Fatal("Publish() expected error")
	}

	wantKey := "err-app/9.9.9/app.zip"
	if !strings.Contains(err.Error(), wantKey) {
		t.Fatalf("Publish() error = %v, want substring %q", err, wantKey)
	}
}
