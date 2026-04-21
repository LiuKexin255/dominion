package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	root := newBazelWorkspace(t)

	validPath := filepath.Join(root, "testdata", "valid.yaml")
	want := &Config{
		Name:        "game-session-large-test",
		Description: "game-session HTTP REST interface test",
		Suites: []*Suite{
			{
				Name:   "default",
				Env:    "game.lt",
				Deploy: "//projects/game/testplan/test_deploy.yaml",
				Endpoint: map[string]Endpoints{
					"http": {
						"public": "https://game.liukexin.com",
					},
				},
				Cases: []string{
					"//projects/game/testplan:testplan_test",
				},
			},
		},
	}

	got, err := Parse(validPath)
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse() = %+v, want %+v", got, want)
	}
}

func TestParse_MissingName(t *testing.T) {
	root := newBazelWorkspace(t)

	path := filepath.Join(root, "testdata", "missing_name.yaml")
	got, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if got.Name != "" {
		t.Errorf("Parse() Name = %q, want empty", got.Name)
	}
}

func TestParse_EmptySuites(t *testing.T) {
	root := newBazelWorkspace(t)

	path := filepath.Join(root, "testdata", "empty_suites.yaml")
	got, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if len(got.Suites) != 0 {
		t.Errorf("Parse() Suites = %d, want 0", len(got.Suites))
	}
}

func TestParse_InvalidEndpointName(t *testing.T) {
	root := newBazelWorkspace(t)

	path := filepath.Join(root, "testdata", "invalid_endpoint_name.yaml")
	got, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if len(got.Suites) == 0 {
		t.Fatal("Parse() returned no suites")
	}
}

func TestParse_SuiteValidation(t *testing.T) {
	root := newBazelWorkspace(t)

	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "missing suite env",
			yaml: `name: test
suites:
  - name: default
    deploy: //deploy.yaml
    cases:
      - //test:test
`,
		},
		{
			name: "missing suite deploy",
			yaml: `name: test
suites:
  - name: default
    env: game.lt
    cases:
      - //test:test
`,
		},
		{
			name: "missing suite cases",
			yaml: `name: test
suites:
  - name: default
    env: game.lt
    deploy: //deploy.yaml
`,
		},
		{
			name: "missing suite name",
			yaml: `name: test
suites:
  - env: game.lt
    deploy: //deploy.yaml
    cases:
      - //test:test
`,
		},
		{
			name: "valid minimal suite",
			yaml: `name: test
suites:
  - name: default
    env: game.lt
    deploy: //deploy.yaml
    cases:
      - //test:test
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(root, "testdata", tt.name+".yaml")
			if err := os.WriteFile(filePath, []byte(tt.yaml), 0o644); err != nil {
				t.Fatalf("WriteFile() failed: %v", err)
			}

			_, err := Parse(filePath)
			if err != nil {
				t.Fatalf("Parse() unexpected error: %v", err)
			}
		})
	}
}

func newBazelWorkspace(t *testing.T) string {
	t.Helper()

	srcRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "MODULE.bazel"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	copyDir(t, filepath.Join(srcRoot, "testdata"), filepath.Join(root, "testdata"))
	withWorkingDir(t, root)
	return root
}

func copyDir(t *testing.T, src string, dst string) {
	t.Helper()

	err := filepath.Walk(src, func(srcPath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(src, srcPath)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(dstPath, os.ModePerm)
		}

		raw, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), os.ModePerm); err != nil {
			return err
		}

		return os.WriteFile(dstPath, raw, info.Mode())
	})
	if err != nil {
		t.Fatalf("copyDir() failed: %v", err)
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
