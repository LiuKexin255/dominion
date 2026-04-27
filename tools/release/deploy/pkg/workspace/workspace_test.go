package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"dominion/tools/release/deploy/pkg/workspace"
)

func TestRoot(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{name: "accepts MODULE.bazel"},
		{name: "outside repo returns error", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			nested := filepath.Join(root, "apps", "svc", "deploy")
			if err := os.MkdirAll(nested, 0o755); err != nil {
				t.Fatalf("MkdirAll() failed: %v", err)
			}

			if !tt.wantErr {
				if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
					t.Fatalf("WriteFile() failed: %v", err)
				}
			}

			cwd := nested
			if tt.wantErr {
				cwd = t.TempDir()
			}
			withWorkingDir(t, cwd)

			got, err := workspace.Root()
			if tt.wantErr {
				if err == nil {
					t.Fatal("Root() succeeded unexpectedly")
				}
				return
			}
			if err != nil {
				t.Fatalf("Root() failed: %v", err)
			}
			if got != root {
				t.Fatalf("Root() = %q, want %q", got, root)
			}
		})
	}
}

func TestWorking(t *testing.T) {
	root, nested := newWorkspaceRepo(t)
	_ = root
	withWorkingDir(t, nested)

	got, err := workspace.Working()
	if err != nil {
		t.Fatalf("Working() failed: %v", err)
	}
	if got != nested {
		t.Fatalf("Working() = %q, want %q", got, nested)
	}
}

func TestResolvePath(t *testing.T) {
	root, nested := newWorkspaceRepo(t)
	withWorkingDir(t, nested)

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "workspace prefix resolves from discovered Bazel root",
			path: "//tools/release/deploy/deploy.yaml",
			want: filepath.Join(root, "tools/release/deploy/deploy.yaml"),
		},
		{
			name: "relative path resolves from cwd",
			path: "tools/release/deploy/deploy.yaml",
			want: filepath.Join(nested, "tools/release/deploy/deploy.yaml"),
		},
		{
			name: "absolute path is preserved",
			path: "/tmp/deploy.yaml",
			want: "/tmp/deploy.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := workspace.ResolvePath(tt.path)
			if got != tt.want {
				t.Fatalf("ResolvePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestToURI(t *testing.T) {
	root, nested := newWorkspaceRepo(t)
	withWorkingDir(t, nested)

	tests := []struct {
		name    string
		absPath string
		want    string
		wantErr bool
	}{
		{
			name:    "repo root path uses discovered Bazel root",
			absPath: filepath.Join(root, "tools/release/deploy/deploy.yaml"),
			want:    "//tools/release/deploy/deploy.yaml",
		},
		{
			name:    "nested path also uses discovered Bazel root",
			absPath: filepath.Join(nested, "deploy.yaml"),
			want:    "//apps/svc/deploy/deploy.yaml",
		},
		{
			name:    "outside repo returns error",
			absPath: filepath.Join(t.TempDir(), "outside.yaml"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := workspace.ToURI(tt.absPath)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ToURI() succeeded unexpectedly")
				}
				return
			}
			if err != nil {
				t.Fatalf("ToURI() failed: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ToURI(%q) = %q, want %q", tt.absPath, got, tt.want)
			}
		})
	}
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore working dir failed: %v", err)
		}
	})
}

func newWorkspaceRepo(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	nested := filepath.Join(root, "apps", "svc", "deploy")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}

	return root, nested
}
