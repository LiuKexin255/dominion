package imagepush

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type stubV3CommandExecutor struct {
	combinedOutput []byte
	combinedErr    error
	output         []byte
	outputErr      error
	calls          []stubV3CommandCall
}

type stubV3CommandCall struct {
	dir  string
	name string
	args []string
}

func (e *stubV3CommandExecutor) CombinedOutput(_ context.Context, dir string, name string, args ...string) ([]byte, error) {
	e.calls = append(e.calls, stubV3CommandCall{dir: dir, name: name, args: append([]string(nil), args...)})
	return e.combinedOutput, e.combinedErr
}

func (e *stubV3CommandExecutor) Output(_ context.Context, dir string, name string, args ...string) ([]byte, error) {
	e.calls = append(e.calls, stubV3CommandCall{dir: dir, name: name, args: append([]string(nil), args...)})
	return e.output, e.outputErr
}

func TestV3BazelRunner_BuildAndReadMetadata(t *testing.T) {
	workspaceRoot := t.TempDir()
	metadataPath := filepath.Join("bazel-bin", "pkg", "metadata.json")
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "bazel-bin", "pkg"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() failed: %v", err)
	}
	raw := `{
		"schema_version": "3",
		"app": "myapp",
		"service": "myservice",
		"binary": "mybinary",
		"entrypoint": "/app/main",
		"image_target": "//pkg:my_image",
		"push_target": "//pkg:my_image_push",
		"repository": "registry.liukexin.com/myapp/myservice",
		"tag": "latest"
	}`
	if err := os.WriteFile(filepath.Join(workspaceRoot, metadataPath), []byte(raw), 0o644); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}

	exec := &stubV3CommandExecutor{output: []byte(filepath.ToSlash(metadataPath) + "\n")}
	runner := &V3BazelRunner{workspaceRoot: workspaceRoot, exec: exec}

	got, err := runner.BuildAndReadMetadata(context.Background(), " //pkg:metadata ")
	if err != nil {
		t.Fatalf("BuildAndReadMetadata() failed: %v", err)
	}

	want := &Metadata{
		SchemaVersion: "3",
		App:           "myapp",
		Service:       "myservice",
		Binary:        "mybinary",
		Entrypoint:    "/app/main",
		ImageTarget:   "//pkg:my_image",
		PushTarget:    "//pkg:my_image_push",
		Repository:    "registry.liukexin.com/myapp/myservice",
		Tag:           "latest",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildAndReadMetadata() = %+v, want %+v", got, want)
	}
}

func TestV3BazelRunner_BuildAndReadMetadata_BuildError(t *testing.T) {
	exec := &stubV3CommandExecutor{combinedOutput: []byte("build failed"), combinedErr: errors.New("exit 1")}
	runner := &V3BazelRunner{workspaceRoot: t.TempDir(), exec: exec}

	_, err := runner.BuildAndReadMetadata(context.Background(), "//pkg:metadata")
	if err == nil {
		t.Fatal("BuildAndReadMetadata() succeeded unexpectedly")
	}
	if !contains(err.Error(), "build failed") {
		t.Fatalf("BuildAndReadMetadata() err = %v, want error containing 'build failed'", err)
	}
}

func TestV3BazelRunner_BuildAndReadMetadata_InvalidJSON(t *testing.T) {
	workspaceRoot := t.TempDir()
	metadataPath := filepath.Join("bazel-bin", "pkg", "metadata.json")
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "bazel-bin", "pkg"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, metadataPath), []byte("{"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	exec := &stubV3CommandExecutor{output: []byte(filepath.ToSlash(metadataPath) + "\n")}
	runner := &V3BazelRunner{workspaceRoot: workspaceRoot, exec: exec}

	_, err := runner.BuildAndReadMetadata(context.Background(), "//pkg:metadata")
	if err == nil {
		t.Fatal("BuildAndReadMetadata() succeeded unexpectedly")
	}
}

func TestV3BazelRunner_RunPush(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{name: "success"},
		{name: "error", err: errors.New("exit 1"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &stubV3CommandExecutor{combinedErr: tt.err}
			runner := &V3BazelRunner{workspaceRoot: t.TempDir(), exec: exec}

			err := runner.RunPush(context.Background(), "//pkg:my_image_push")
			if tt.wantErr && err == nil {
				t.Fatal("RunPush() succeeded unexpectedly")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("RunPush() failed: %v", err)
			}
		})
	}
}

func TestV3BazelRunner_ReadDigest(t *testing.T) {
	workspaceRoot := t.TempDir()
	imagePath := filepath.Join("bazel-bin", "pkg", "image")
	if err := os.MkdirAll(filepath.Join(workspaceRoot, imagePath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, imagePath, imageIndexFileName), []byte(`{"manifests":[{"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`), 0o644); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}
	exec := &stubV3CommandExecutor{output: []byte(filepath.ToSlash(imagePath) + "\n")}
	runner := &V3BazelRunner{workspaceRoot: workspaceRoot, exec: exec}

	got, err := runner.ReadDigest(context.Background(), "//pkg:my_image")
	if err != nil {
		t.Fatalf("ReadDigest() failed: %v", err)
	}

	want := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if got != want {
		t.Fatalf("ReadDigest() = %q, want %q", got, want)
	}
}

func TestV3BazelRunner_ReadDigest_NoManifests(t *testing.T) {
	runner := newDigestTestRunner(t, `{"manifests":[]}`)

	_, err := runner.ReadDigest(context.Background(), "//pkg:my_image")
	if err == nil {
		t.Fatal("ReadDigest() succeeded unexpectedly")
	}
}

func TestV3BazelRunner_ReadDigest_EmptyDigest(t *testing.T) {
	runner := newDigestTestRunner(t, `{"manifests":[{"digest":""}]}`)

	_, err := runner.ReadDigest(context.Background(), "//pkg:my_image")
	if err == nil {
		t.Fatal("ReadDigest() succeeded unexpectedly")
	}
}

func newDigestTestRunner(t *testing.T, index string) *V3BazelRunner {
	t.Helper()

	workspaceRoot := t.TempDir()
	imagePath := filepath.Join("bazel-bin", "pkg", "image")
	if err := os.MkdirAll(filepath.Join(workspaceRoot, imagePath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, imagePath, imageIndexFileName), []byte(index), 0o644); err != nil {
		t.Fatalf("os.WriteFile() failed: %v", err)
	}

	exec := &stubV3CommandExecutor{output: []byte(filepath.ToSlash(imagePath) + "\n")}
	return &V3BazelRunner{workspaceRoot: workspaceRoot, exec: exec}
}
