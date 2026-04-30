package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestStageFrontend(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) (srcDir string, outDir string)
		wantErr bool
		check   func(t *testing.T, srcDir string, outDir string)
	}{
		{
			name: "empty source directory",
			setup: func(t *testing.T) (string, string) {
				t.Helper()
				srcDir := t.TempDir()
				outDir := t.TempDir()
				return srcDir, outDir
			},
			wantErr: false,
			check: func(t *testing.T, srcDir string, outDir string) {
				t.Helper()
				info, err := os.Stat(outDir)
				if err != nil {
					t.Fatalf("frontend_dist directory does not exist: %v", err)
				}
				if !info.IsDir() {
					t.Fatalf("frontend_dist is not a directory")
				}
			},
		},
		{
			name: "nested directory structure",
			setup: func(t *testing.T) (string, string) {
				t.Helper()
				srcDir := t.TempDir()
				outDir := t.TempDir()

				dirs := []string{
					"assets/images",
					"css",
					"js",
				}
				for _, d := range dirs {
					if err := os.MkdirAll(filepath.Join(srcDir, d), 0o755); err != nil {
						t.Fatalf("create dir %s: %v", d, err)
					}
				}

				files := map[string]string{
					"index.html":             "<html></html>",
					"assets/images/logo.png": "png-data",
					"css/style.css":          "body{}",
					"js/app.js":              "console.log()",
				}
				for name, content := range files {
					if err := os.WriteFile(filepath.Join(srcDir, name), []byte(content), 0o644); err != nil {
						t.Fatalf("write file %s: %v", name, err)
					}
				}

				return srcDir, outDir
			},
			wantErr: false,
			check: func(t *testing.T, srcDir string, outDir string) {
				t.Helper()
				wantFiles := []string{
					"index.html",
					"assets/images/logo.png",
					"css/style.css",
					"js/app.js",
				}
				for _, f := range wantFiles {
					got, err := os.ReadFile(filepath.Join(outDir, f))
					if err != nil {
						t.Fatalf("file %s does not exist in output: %v", f, err)
					}
					want, err := os.ReadFile(filepath.Join(srcDir, f))
					if err != nil {
						t.Fatalf("file %s does not exist in source: %v", f, err)
					}
					if string(got) != string(want) {
						t.Fatalf("file %s content mismatch: got %q, want %q", f, got, want)
					}
				}
			},
		},
		{
			name: "files with special characters in name",
			setup: func(t *testing.T) (string, string) {
				t.Helper()
				srcDir := t.TempDir()
				outDir := t.TempDir()

				files := map[string]string{
					"app.abc123.js":    "js-bundle",
					"style.def456.css": "css-bundle",
					"index.html":       "<html>special</html>",
				}
				for name, content := range files {
					if err := os.WriteFile(filepath.Join(srcDir, name), []byte(content), 0o644); err != nil {
						t.Fatalf("write file %s: %v", name, err)
					}
				}

				return srcDir, outDir
			},
			wantErr: false,
			check: func(t *testing.T, srcDir string, outDir string) {
				t.Helper()
				files := []string{
					"app.abc123.js",
					"style.def456.css",
					"index.html",
				}
				for _, f := range files {
					got, err := os.ReadFile(filepath.Join(outDir, f))
					if err != nil {
						t.Fatalf("file %s missing from output: %v", f, err)
					}
					if len(got) == 0 {
						t.Fatalf("file %s is empty in output", f)
					}
				}
			},
		},
		{
			name: "nonexistent source directory",
			setup: func(t *testing.T) (string, string) {
				t.Helper()
				srcDir := filepath.Join(t.TempDir(), "nonexistent")
				outDir := t.TempDir()
				return srcDir, outDir
			},
			wantErr: true,
		},
		{
			name: "deterministic output",
			setup: func(t *testing.T) (string, string) {
				t.Helper()
				srcDir := t.TempDir()

				if err := os.WriteFile(filepath.Join(srcDir, "test.txt"), []byte("hello"), 0o644); err != nil {
					t.Fatalf("write test.txt: %v", err)
				}

				outDir1 := t.TempDir()
				if err := copyDirTree(srcDir, outDir1); err != nil {
					t.Fatalf("first copy: %v", err)
				}

				outDir2 := t.TempDir()
				if err := copyDirTree(srcDir, outDir2); err != nil {
					t.Fatalf("second copy: %v", err)
				}

				return srcDir, outDir1
			},
			wantErr: false,
			check: func(t *testing.T, srcDir string, outDir string) {
				t.Helper()
				var files []string
				filepath.WalkDir(outDir, func(path string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !d.IsDir() {
						rel, _ := filepath.Rel(outDir, path)
						files = append(files, rel)
					}
					return nil
				})
				sort.Strings(files)

				if len(files) != 1 || files[0] != "test.txt" {
					t.Fatalf("unexpected files: %v", files)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			srcDir, outDir := tt.setup(t)

			// when
			err := stageFrontend(srcDir, outDir)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil && err == nil {
				tt.check(t, srcDir, outDir)
			}
		})
	}
}
